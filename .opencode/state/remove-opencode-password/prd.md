# PRD: Remove OPENCODE_SERVER_PASSWORD Feature

**Date:** 2026-03-23

---

## Problem Statement

### What problem are we solving?

All minion pods are failing health checks and never completing tasks. Root cause analysis via Kubernetes pod testing revealed that when the `OPENCODE_SERVER_PASSWORD` environment variable is set, OpenCode hangs for 90+ seconds during startup before becoming ready to serve HTTP requests. The devbox entrypoint script has a 60-second health check timeout, causing all minion pods to exit with failure before OpenCode finishes initializing.

**Impact:**
- **100% minion task failure rate** - No minion tasks can complete successfully
- **User experience:** Discord users receive failure notifications for all `@minion` requests
- **Business impact:** Core product feature (AI-powered PR generation) is completely broken

Test evidence:
- Without `OPENCODE_SERVER_PASSWORD`: OpenCode ready in 2 seconds ✓
- With `OPENCODE_SERVER_PASSWORD`: OpenCode ready in 90+ seconds ✗

### Why now?

This is a **P0 production incident** blocking all minion functionality. The password feature was introduced in the recent "minion-observability-fix" work (commits cd3e452 through b0217fb) to secure SSE connections between orchestrator and minion pods. While well-intentioned, it introduced an unexpected OpenCode startup delay that makes the feature unusable.

**Cost of inaction:** Minions remain completely broken, users cannot use the core product feature.

### Who is affected?

- **Primary users:** Discord users attempting to create PRs via `@minion` commands (all requests fail)
- **Secondary users:** Development team debugging failed minion pods, dealing with user complaints
- **System impact:** Control panel shows all minions in "failed" state, orchestrator logs filled with health check timeouts

---

## Proposed Solution

### Overview

Remove the `OPENCODE_SERVER_PASSWORD` authentication feature entirely. This includes:
1. Removing the `opencode_password` column from the database (via migration 006)
2. Removing password generation, storage, and validation logic from orchestrator
3. Removing password authentication from SSE client connections
4. Removing `OPENCODE_SERVER_PASSWORD` validation from devbox entrypoint
5. Updating documentation to reflect unsecured SSE architecture

The SSE connection will become unsecured (no HTTP Basic Auth), which is acceptable because:
- Pod-to-pod communication is within a trusted Kubernetes namespace
- Network policies restrict cross-namespace traffic
- Minion pods are ephemeral (single task lifetime, 5-15 minutes)
- No external ingress exists to minion pods
- Worst-case exposure: attacker with pod network access could read OpenCode event stream for one task

Future authentication improvements (if needed) can use Kubernetes-native mechanisms like service accounts, RBAC, or mutual TLS rather than environment variable passwords.

---

## End State

When this PRD is complete, the following will be true:

- [ ] Minion pods start successfully and pass health checks within 10 seconds
- [ ] OpenCode serves HTTP requests immediately after printing "server listening"
- [ ] SSE streaming from minion pods to orchestrator works (unsecured connection)
- [ ] Database migration 006 removes `opencode_password` column
- [ ] No code references `OPENCODE_SERVER_PASSWORD`, `opencode_password`, or `OpencodePassword`
- [ ] Orchestrator builds without errors
- [ ] Devbox image builds without errors
- [ ] All Go tests pass (`go test ./...` in orchestrator and discord-bot)
- [ ] AGENTS.md documentation updated to remove password lifecycle sections
- [ ] Migration 005 remains in ConfigMap for historical audit trail

---

## Success Metrics

### Quantitative
| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Minion health check pass rate | 0% (all fail) | 100% | Manual Discord test after deployment |
| OpenCode startup time (with no password) | 2s (known) | < 10s | Docker test: `opencode serve` to `/global/health` response |
| Minion task completion rate | 0% (all fail) | > 0% (any success) | Trigger Discord minion, verify PR created |

### Qualitative
- Minion pods reach "Running" status in Kubernetes
- Control panel shows successful minion events
- Discord users receive PR URLs instead of failure messages

---

## Acceptance Criteria

### Database Migration
- [ ] Migration `006_remove_opencode_password.sql` exists and drops `opencode_password` column
- [ ] Migration uses `DROP COLUMN IF EXISTS` for idempotency
- [ ] Migration added to `infra/configmap.yaml` db-migrations ConfigMap
- [ ] Migration 005 remains in ConfigMap (not removed)

### Orchestrator Code
- [ ] `OpencodePassword` field removed from `Minion` struct in `orchestrator/internal/db/minions.go`
- [ ] All 8 database query methods no longer SELECT or Scan `opencode_password` column
- [ ] `StorePassword()` and `ClearPassword()` methods removed from `orchestrator/internal/db/minions.go`
- [ ] Password generation and storage logic removed from `orchestrator/internal/spawner/spawner.go`
- [ ] `OpencodePassword` field removed from `SpawnParams` in `orchestrator/internal/k8s/pods.go`
- [ ] `OPENCODE_SERVER_PASSWORD` env var removed from `buildEnvVars()` in `orchestrator/internal/k8s/pods.go`
- [ ] `password` parameter removed from SSE client methods: `Connect()`, `connectWithRetry()`, `streamEvents()` in `orchestrator/internal/streaming/sse.go`
- [ ] `password` field removed from `connection` struct in `orchestrator/internal/streaming/sse.go`
- [ ] HTTP Basic Auth header logic (Base64 encoding and Authorization header) removed from `streamEvents()` in `orchestrator/internal/streaming/sse.go`
- [ ] Reconnection logic in `orchestrator/cmd/orchestrator/main.go` updated to call `Connect()` without password
- [ ] `ClearPassword()` method removed from `MinionUpdater` interface in `orchestrator/internal/watchdog/watchdog.go`
- [ ] `ClearPassword()` calls removed from `orchestrator/internal/watchdog/watchdog.go` (lines ~228, ~267 in checkFailedPods and checkClarificationTimeouts)
- [ ] `ClearPassword()` calls removed from `orchestrator/internal/api/minions.go` (lines ~452, ~554 in HandleDelete and HandleCallback)

### Devbox Code
- [ ] `OPENCODE_SERVER_PASSWORD` removed from required env var list in `devbox/entrypoint.sh`
- [ ] `OPENCODE_SERVER_PASSWORD` documentation removed from header comment in `devbox/entrypoint.sh`

### Tests
- [ ] `StorePassword` test block (lines 74-81) removed from `orchestrator/internal/db/minions_test.go`
- [ ] Mock `StorePassword` method removed from `mockMinionUpdater` struct in `orchestrator/internal/db/minions_test.go`
- [ ] `OpencodePassword` assertions removed from `orchestrator/internal/spawner/spawner_test.go`
- [ ] Mock `StorePassword` method (line 64) removed from `orchestrator/internal/spawner/spawner_test.go`
- [ ] Password call count assertions (lines 665, 725) removed from `orchestrator/internal/spawner/spawner_test.go`
- [ ] `TestSpawner_HandlesStorePasswordFailure` test function (line 744) deleted from `orchestrator/internal/spawner/spawner_test.go`
- [ ] SSE client test signatures updated to remove password parameter in `orchestrator/internal/streaming/sse_test.go`
- [ ] All tests pass: `cd orchestrator && go test ./...`
- [ ] Discord bot builds: `cd discord-bot && go build ./...`

### Documentation
- [ ] AGENTS.md line 93 header text updated to note unsecured SSE (change "with per-pod UUID password authentication" to "over unsecured HTTP (trusted pod network)")
- [ ] AGENTS.md "Minion Observability & Security" section updated to remove password lifecycle
- [ ] AGENTS.md "Password Lifecycle" diagram removed
- [ ] AGENTS.md "SSE Connection Troubleshooting" table updated to remove password-related errors
- [ ] AGENTS.md "Orchestrator Restart Behavior" updated to remove password filtering
- [ ] AGENTS.md "Deployment Notes" updated to remove migration 005 reference

### Build & Deployment Validation
- [ ] Orchestrator builds: `cd orchestrator && go build ./...`
- [ ] Devbox image builds: `docker build -t devbox:test ./devbox`
- [ ] OpenCode startup test passes: devbox container starts OpenCode and serves `/global/health` within 5 seconds

### Integration Testing
- [ ] Pod spawn validation: Spawn test pod without `OPENCODE_SERVER_PASSWORD`, verify health check passes within 10s
- [ ] Verify `kubectl describe pod` shows env vars exclude `OPENCODE_SERVER_PASSWORD`
- [ ] SSE connection test: Trigger Discord minion, verify control panel receives events via SSE proxy
- [ ] Check orchestrator logs confirm "SSE connection established" without auth errors
- [ ] Orchestrator restart test: Restart orchestrator pod while minion is running, verify SSE reconnects without password filtering

---

## Technical Context

### Existing Patterns

**Database migrations:**
- Pattern: `schema/migrations/NNN_description.sql` - Sequential numbered migrations
- Orchestrator applies migrations automatically on startup
- ConfigMap: `infra/configmap.yaml` contains all migration SQL for pod access
- Migration 005 added `opencode_password` column, now being removed by migration 006

**SSE streaming architecture:**
- Pattern: `orchestrator/internal/streaming/sse.go` - Client connects to pod's OpenCode `/events` endpoint
- Orchestrator → Minion Pod HTTP connection on port 4096
- Current: HTTP Basic Auth with `opencode:<password>`
- After: Unsecured HTTP (trusted pod network)

**Spawner password flow (current, being removed):**
- `orchestrator/internal/spawner/spawner.go:194-229` - Generates UUID password if not exists
- `orchestrator/internal/db/minions.go:1055` - Stores password in DB
- `orchestrator/internal/k8s/pods.go:428` - Passes to pod via `OPENCODE_SERVER_PASSWORD` env var
- `devbox/entrypoint.sh:51` - Validates env var exists before starting OpenCode

### Key Files

**Orchestrator (Go):**
- `orchestrator/internal/db/minions.go` - Minion struct, query methods, password storage/clearing (56 lines to modify)
- `orchestrator/internal/spawner/spawner.go` - Password generation and pod spawning (35 lines to modify)
- `orchestrator/internal/k8s/pods.go` - Pod manifest creation, env vars (10 lines to modify)
- `orchestrator/internal/streaming/sse.go` - SSE client, HTTP auth, connection struct (35 lines to modify)
- `orchestrator/cmd/orchestrator/main.go` - Reconnection logic (5 lines to modify)
- `orchestrator/internal/watchdog/watchdog.go` - Password cleanup interface and call sites (15 lines to modify)
- `orchestrator/internal/api/minions.go` - Password cleanup calls in HandleDelete and HandleCallback (10 lines to modify)

**Orchestrator tests (Go):**
- `orchestrator/internal/db/minions_test.go` - Mock password operations and StorePassword test block (15 lines to modify)
- `orchestrator/internal/spawner/spawner_test.go` - Password assertions, mock methods, and TestSpawner_HandlesStorePasswordFailure deletion (25 lines to modify)
- `orchestrator/internal/streaming/sse_test.go` - SSE client test signatures (5 lines to modify)

**Devbox (Shell):**
- `devbox/entrypoint.sh` - Env var validation, health check (5 lines to modify)

**Database:**
- `schema/migrations/006_remove_opencode_password.sql` - New migration to drop column
- `infra/configmap.yaml` - Add migration 006, keep 005

**Documentation:**
- `AGENTS.md` - Remove password lifecycle sections (~80 lines to modify)

### System Dependencies

**Runtime dependencies:**
- PostgreSQL database with minions table (migration will modify schema)
- Kubernetes cluster with minions namespace (pod-to-pod networking must allow unsecured HTTP)
- OpenCode CLI 1.3.0+ (serves `/global/health` endpoint)

**Build dependencies:**
- Go 1.22+ (orchestrator, discord-bot)
- Docker (devbox image)
- GitHub Actions CI (builds images on version tags)

### Data Model Changes

**Before (migration 005):**
```sql
CREATE TABLE minions (
  id UUID PRIMARY KEY,
  -- ... other columns ...
  opencode_password TEXT NULL  -- Per-minion UUID for SSE auth
);
```

**After (migration 006):**
```sql
CREATE TABLE minions (
  id UUID PRIMARY KEY,
  -- ... other columns ...
  -- opencode_password column removed
);
```

**Migration 006:**
```sql
ALTER TABLE minions DROP COLUMN IF EXISTS opencode_password;
```

**Data backfill:** Not needed - column is dropped, existing passwords discarded (acceptable, minions are ephemeral)

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| SSE connection insecurity (no auth) | High | Medium | Acceptable for v1 - pod network is namespace-isolated. Blast radius: Any compromised minion pod can read SSE streams from other active minions (token usage, task descriptions, repo names). Future: implement k8s service accounts or NetworkPolicies to restrict pod-to-pod traffic. Monitor orchestrator logs for unauthorized SSE connections. |
| Migration 006 fails in production | Low | High | Use `DROP COLUMN IF EXISTS` for idempotency. Test migration locally before deploy |
| Breaking change for in-flight minions | Low | Low | Minions are short-lived (5-15 min). Deploy during low-traffic window. Any in-flight minions will complete with old orchestrator |
| Missed code references to password | Med | Med | Use grep to find all references: `OPENCODE_SERVER_PASSWORD\|opencode_password\|OpencodePassword`. Run full test suite before merge. Oracle review identified 5 additional code locations not in original PRD. |
| OpenCode still slow without password (our diagnosis wrong) | Low | High | Validated via Kubernetes pod testing - 2s startup without password, 90s with password. If still slow, see rollback plan. |

---

## Alternatives Considered

### Alternative 1: Increase health check timeout to 120 seconds
- **Description:** Keep password feature, increase `HEALTH_TIMEOUT=60` to `HEALTH_TIMEOUT=120` in devbox entrypoint
- **Pros:** Preserves SSE authentication, minimal code change (1 line)
- **Cons:** 
  - Masks underlying OpenCode bug (why does password cause 90s delay?)
  - Minion pods take 2+ minutes to start (poor UX)
  - Health check may still timeout if OpenCode takes longer in different environments
  - Doesn't fix root cause
- **Decision:** Rejected - Unacceptable startup time, doesn't address OpenCode bug

### Alternative 2: Use a shared static password for all minions
- **Description:** Replace per-minion UUID passwords with single static password from Kubernetes Secret
- **Pros:** Simpler than per-minion UUIDs, might avoid OpenCode startup delay (unclear why password causes delay)
- **Cons:**
  - Doesn't eliminate OpenCode startup delay (likely same issue)
  - Shared secret increases blast radius if compromised
  - Still requires testing to confirm it solves the problem
- **Decision:** Rejected - Unlikely to fix startup delay, added complexity for minimal security benefit

### Alternative 3: Investigate and fix OpenCode startup delay
- **Description:** Debug why `OPENCODE_SERVER_PASSWORD` env var causes 90+ second startup, contribute fix to OpenCode upstream
- **Pros:** Fixes root cause, benefits OpenCode community, preserves authentication
- **Cons:**
  - Requires deep OpenCode internals knowledge
  - Uncertain timeline (days/weeks to debug and upstream fix)
  - Minions remain broken until fix is available
  - Blocks critical product feature
- **Decision:** Rejected for v1 - Too slow. Can revisit if SSE authentication becomes critical in future

### Alternative 4: Disable SSE streaming entirely
- **Description:** Remove SSE client from orchestrator, rely only on callback API for minion status
- **Pros:** Eliminates SSE complexity, passwords, and OpenCode startup issue
- **Cons:**
  - Loses real-time event streaming to control panel (major UX degradation)
  - Loses token usage tracking (cost visibility)
  - Loses mid-task observability (debugging harder)
- **Decision:** Rejected - SSE streaming is valuable, unsecured connection is acceptable

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Re-implementing SSE authentication** - Future work if needed. Options include Kubernetes service accounts, mutual TLS, or request signing. Defer until security requirements justify the complexity.

- **NetworkPolicy restrictions for SSE** - Future work. Could restrict OpenCode port 4096 to only accept connections from orchestrator pod IPs using Kubernetes NetworkPolicies. Defer until security review recommends it. Would provide network-level security without password auth overhead.

- **Adding OpenCode startup time monitoring** - Good idea (metrics for health check duration), but separate concern. Track in future observability work.

- **Removing migration 005 from ConfigMap** - Keep for historical audit trail. Removing provides no benefit and loses migration history.

- **Investigating root cause of OpenCode password delay** - Out of scope for immediate fix. Could file issue with OpenCode upstream if needed later.

- **Adding Kubernetes readiness probes to minion pods** - Current entrypoint exit-on-failure pattern works. Readiness probes would add complexity without clear benefit for ephemeral pods.

- **Cleaning up `.opencode/state/minion-observability-fix/` directory** - State files are historical reference. Not worth the churn to delete.

- **SSE connection authentication metrics** - Out of scope. We lose 401 error logging, but gain simpler debugging (fewer failure modes). If needed, add connection success/failure metrics in future observability work.

- **Reconciler password cleanup** - Not needed; reconciliation runs before password removal, and orphaned minions will have NULL passwords after migration.

---

## Implementation Sequence

This section defines the dependency order for changes to minimize risk and ensure successful deployment.

### Phase 1: Database (can be done in parallel with Phase 2)
1. Create migration `006_remove_opencode_password.sql`
2. Add to `infra/configmap.yaml`

### Phase 2: Code Removal
1. Remove password generation in `spawner.go` (lines 194-229)
2. Remove password storage/clearing methods in `minions.go` (`StorePassword`, `ClearPassword`)
3. Remove password field from `Minion` struct and update all 8 query methods
4. Remove password parameter from SSE client methods (`Connect`, `connectWithRetry`, `streamEvents`)
5. Remove `password` field from `connection` struct in `streaming/sse.go`
6. Remove HTTP Basic Auth header logic (Base64 encoding, Authorization header) from `streamEvents`
7. Remove env var from `pods.go` (`buildEnvVars`, `SpawnParams`) and `entrypoint.sh`
8. Remove password cleanup calls in `api/minions.go` (HandleDelete, HandleCallback)
9. Remove password cleanup calls in `watchdog/watchdog.go` (checkFailedPods, checkClarificationTimeouts)
10. Remove `ClearPassword` method from `MinionUpdater` interface in `watchdog.go`

### Phase 3: Test Updates
1. Remove password assertions from `spawner_test.go`
2. Delete `TestSpawner_HandlesStorePasswordFailure` test function
3. Remove mock `StorePassword` methods and call count assertions
4. Remove `StorePassword` test block from `minions_test.go`
5. Update SSE client test signatures in `sse_test.go`
6. Run full test suite: `cd orchestrator && go test ./...`

### Phase 4: Documentation
1. Update AGENTS.md line 93 header text
2. Remove "Password Lifecycle" diagram
3. Update "SSE Connection Troubleshooting" table
4. Update "Orchestrator Restart Behavior" section
5. Remove migration 005 reference from "Deployment Notes"

### Phase 5: Validation
1. Build orchestrator: `cd orchestrator && go build ./...`
2. Build discord-bot: `cd discord-bot && go build ./...`
3. Build devbox image: `docker build -t devbox:test ./devbox`
4. Test OpenCode startup time (< 5s without password)
5. Run integration tests (pod spawn, SSE connection, orchestrator restart)
6. Manual Discord minion test

---

## Rollback Plan

If OpenCode startup remains slow post-deployment or other issues arise:

### Scenario 1: OpenCode still slow (diagnosis was wrong)

**Immediate workaround:**
1. Increase `HEALTH_TIMEOUT` to 120s in `devbox/entrypoint.sh` (temporary mitigation)
2. Deploy updated devbox image

**Investigation:**
1. Check OpenCode logs for root cause (not password-related)
2. Test with different OpenCode versions
3. File issue with OpenCode upstream if needed

**Revert (if investigation takes too long):**
1. Create migration `007_re_add_opencode_password.sql`:
   ```sql
   ALTER TABLE minions ADD COLUMN IF NOT EXISTS opencode_password TEXT NULL;
   ```
2. Revert code changes via `git revert`
3. Deploy reverted code + migration 007
4. Note: In-flight minions will fail (acceptable, low probability during deployment window)

### Scenario 2: SSE security becomes a concern

**Forward fix (do NOT revert password auth):**
1. Implement NetworkPolicy restrictions (restrict port 4096 to orchestrator IPs)
2. Alternative: Implement Kubernetes service account-based auth
3. Do NOT re-add password authentication (defeats purpose of this fix)

### Scenario 3: Migration 006 fails in production

**Mitigation:**
1. `DROP COLUMN IF EXISTS` is idempotent - safe to retry
2. Check PostgreSQL logs for permission errors or lock contention
3. If column doesn't exist, migration succeeds as no-op

**Recovery:**
1. Migration runs automatically on orchestrator startup
2. If orchestrator crashes during migration, restart will retry (idempotent)
3. No manual intervention needed

---

## Interface Specifications

### Database Migration

**File:** `schema/migrations/006_remove_opencode_password.sql`

```sql
-- Remove opencode_password column (no longer needed, causes OpenCode startup delays)
-- Migration: 006_remove_opencode_password.sql
-- Reason: OPENCODE_SERVER_PASSWORD env var causes 90+ second OpenCode startup delay,
--         breaking health checks. SSE authentication not needed (trusted pod network).

ALTER TABLE minions DROP COLUMN IF EXISTS opencode_password;
```

### SSE Client API Changes

**Before:**
```go
func (c *SSEClient) Connect(ctx context.Context, minionID uuid.UUID, podName string, password string)
```

**After:**
```go
func (c *SSEClient) Connect(ctx context.Context, minionID uuid.UUID, podName string)
```

**Behavior change:**
- HTTP request to `http://<pod-ip>:4096/events` will no longer include `Authorization: Basic <base64(opencode:password)>` header
- Connection is unsecured

### Minion Struct Changes

**Before:**
```go
type Minion struct {
    ID               uuid.UUID
    // ... other fields ...
    OpencodePassword *string
}
```

**After:**
```go
type Minion struct {
    ID               uuid.UUID
    // ... other fields ...
    // OpencodePassword removed
}
```

### Devbox Entrypoint Changes

**Before:**
```bash
validate_env() {
    local missing=()
    [[ -z "${OPENCODE_SERVER_PASSWORD:-}" ]] && missing+=("OPENCODE_SERVER_PASSWORD")
    # ... other checks ...
}
```

**After:**
```bash
validate_env() {
    local missing=()
    # OPENCODE_SERVER_PASSWORD check removed
    # ... other checks ...
}
```

---

## Documentation Requirements

- [x] AGENTS.md updates (part of this PRD implementation):
  - Remove "Minion Observability & Security" → "Password Lifecycle" section
  - Remove password troubleshooting entries from "SSE Connection Troubleshooting" table
  - Update "Orchestrator Restart Behavior" to remove password filtering logic
  - Remove migration 005 reference from "Deployment Notes" (keep migration, remove doc reference)
  - Update SSE architecture description to note unsecured connection

- [ ] No user-facing documentation changes needed (Discord bot usage unchanged)
- [ ] No API documentation changes needed (external API unchanged)
- [ ] No runbook/playbook updates needed (fewer failure modes = less to document)
- [ ] No ADR needed (this PRD serves as decision record)

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| Should we add a comment in AGENTS.md noting that SSE auth was removed due to OpenCode startup issues? | Implementation team | Before merge | Open |
| Do we need to notify any external stakeholders about minions being fixed? | Product owner | After deployment | Open |
| Should we implement NetworkPolicies immediately (Phase 1) or defer to future work? | Security team | Before implementation | Open - PRD recommends defer to future |

---

## Appendix

### Glossary

- **Minion:** Ephemeral Kubernetes pod that clones a repo, runs OpenCode, executes an AI-powered task, and creates a PR
- **OpenCode:** CLI tool that provides an AI coding agent with SSE event streaming
- **SSE (Server-Sent Events):** HTTP streaming protocol used for real-time event delivery from minion pods to orchestrator
- **Devbox:** Container image used for minion pods (includes OpenCode, git, gh CLI, development tools)
- **Health check:** HTTP polling loop in devbox entrypoint that waits for OpenCode to become ready before proceeding with task execution
- **Orchestrator:** Go service that manages minion lifecycle (creates pods, tracks status, streams events)

### References

- Root cause analysis: Kubernetes pod testing logs (this conversation)
- Related commits: cd3e452 through b0217fb (minion-observability-fix branch)
- OpenCode documentation: https://opencode.ai/docs
- Migration 005 (being reverted): `schema/migrations/005_add_opencode_password.sql`

### Test Evidence

**Kubernetes pod test results:**

Test 1 (without password):
```
[2026-03-23T04:14:26Z] Starting OpenCode serve test
[2026-03-23T04:14:27Z] OpenCode started with PID 43
[2026-03-23T04:14:29Z] ✓ Health check SUCCESS after 2s
```

Test 2 (with password):
```
START: 2026-03-23T04:19:11Z
OpenCode PID: 21
Waiting... 10s
Waiting... 20s
...
Waiting... 90s
TIMEOUT at 90s
[Logs show: "opencode server listening on http://0.0.0.0:4096"]
```

**Conclusion:** `OPENCODE_SERVER_PASSWORD` env var causes 90+ second startup delay, breaking 60-second health check.
