# PRD: Minion Observability & Connection Architecture Fix

**Date:** 2026-03-22  
**Status:** Ready for Implementation  
**Urgency:** Critical (Deploy Today)

---

## Problem Statement

### What problem are we solving?

The minions orchestration system has two critical architectural issues preventing real-time observability:

1. **OpenCode SSE Unreachable**: The OpenCode server in minion pods binds to `127.0.0.1:4096`, making it unreachable from the orchestrator pod. Orchestrator cannot stream task events via SSE, leaving the system blind to minion activity.

2. **Control Panel WebSocket DNS Failure**: The control panel attempts to connect to `ws://orchestrator.minions.svc.cluster.local:8080` from the browser. Cluster-internal DNS names are not resolvable from the client, causing WebSocket connections to fail immediately.

**Impact**: Minions execute tasks successfully, but we have zero real-time visibility. Debugging requires log tailing, task completion is opaque, and the control panel UI is non-functional for live monitoring. This blocks developer testing and makes the system unshippable to end users.

**Evidence**:
- Orchestrator logs: `dial tcp 10.244.247.192:4096: connect: connection refused`
- OpenCode logs: `opencode server listening on http://127.0.0.1:4096`
- Control panel browser console: `Failed to create websocket connection`

### Why now?

System is in developer testing phase. Without observability, we cannot validate minion behavior or debug failures. This blocks all further development and testing. Critical to fix before any user-facing release.

### Who is affected?

- **Primary users:** Developer (testing and debugging minions)
- **Secondary users:** Future Discord bot users (blocked until observability works)

---

## Proposed Solution

### Overview

Fix the network binding and authentication issues to restore full observability of minion task execution. This involves two independent but related changes:

1. **OpenCode Server Network Binding**: Change OpenCode from localhost-only to accepting connections from all interfaces within the pod network.

2. **Control Panel Streaming Architecture**: Replace browser-initiated WebSocket with server-side EventSource (SSE) proxied through the Next.js API layer, eliminating the DNS resolution problem.

Both changes require implementing a security layer: per-minion password authentication to prevent unauthorized access to OpenCode servers.

### Architecture Flow (After Fix)

```
┌─────────────────────────────────────────────────────────────────┐
│ Minion Pod (10.244.x.x)                                         │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ OpenCode Server                                            │ │
│  │ Listening: 0.0.0.0:4096                                    │ │
│  │ Auth: HTTP Basic (OPENCODE_SERVER_PASSWORD env var)       │ │
│  │ Endpoint: GET /event (SSE stream)                          │ │
│  └────────────────────────────────────────────────────────────┘ │
│         ▲                                                        │
└─────────┼────────────────────────────────────────────────────────┘
          │ HTTP Basic Auth
          │ Authorization: Basic base64(opencode:<uuid-password>)
          │
┌─────────┴────────────────────────────────────────────────────────┐
│ Orchestrator Pod                                                 │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ SSE Client                                                 │ │
│  │ 1. Generates UUID password                                 │ │
│  │ 2. Stores in DB (minions.opencode_password)               │ │
│  │ 3. Spawns pod with password as env var                    │ │
│  │ 4. Connects to pod /event endpoint with Basic Auth        │ │
│  │ 5. Streams events to WebSocket hub                        │ │
│  └────────────────────────────────────────────────────────────┘ │
│         │                                                         │
│         │ WebSocket                                               │
│         ▼                                                         │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ WebSocket Hub                                              │ │
│  │ ws://orchestrator:8080/api/minions/{id}/stream             │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────┬───────────────────────────────────────┘
                           │ WebSocket (cluster-internal)
                           │
┌──────────────────────────┴───────────────────────────────────────┐
│ Control Panel (Next.js)                                          │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ API Route: /api/minions/[id]/events/stream                │ │
│  │ Server-side WebSocket → SSE transformation                │ │
│  │ Handles: keepalive, client disconnect, error forwarding   │ │
│  └────────────────────────────────────────────────────────────┘ │
│         │ Server-Sent Events                                     │
│         ▼                                                         │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ Browser: EventSource                                       │ │
│  │ GET /api/minions/{id}/events/stream                        │ │
│  │ No DNS issues (same-origin request)                        │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

---

## End State

When this PRD is complete, the following will be true:

- [ ] OpenCode server in minion pods accepts connections from orchestrator pod IP addresses (`0.0.0.0:4096`)
- [ ] Orchestrator successfully establishes SSE connections to all spawned minion pods
- [ ] Orchestrator reconnects to all active minions (pending + running with password) after restart
- [ ] Per-minion UUID passwords are generated, stored in DB BEFORE pod spawn, and used for HTTP Basic Auth
- [ ] Passwords are stored in SSEClient connections map for reconnection scenarios
- [ ] Passwords are cleared from database when minions reach terminal states (completed/failed/terminated)
- [ ] SSE client uses correct endpoint path (`/event` not `/events`)
- [ ] HTTP connection pool supports 50 concurrent SSE streams (MaxIdleConns: 100, MaxConnsPerHost: 50)
- [ ] Control panel displays real-time minion events via EventSource
- [ ] Control panel SSE proxy handles client disconnects without leaking connections
- [ ] Browser console shows no WebSocket-related errors
- [ ] All OpenCode authentication warnings are eliminated from logs
- [ ] Legacy WebSocket configuration endpoint (`/api/ws-config`) is removed
- [ ] Database migration 005 adds `opencode_password` column
- [ ] All 8 database query methods correctly handle the new column
- [ ] Docker images for all 3 services build successfully
- [ ] Go unit tests pass
- [ ] Manual E2E validation confirms: SSE streaming, orchestrator restart reconnection, password cleanup, auth failures (401)

---

## Success Metrics

### Quantitative
| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| SSE connection success rate | 0% (connection refused) | 100% | Orchestrator logs: "SSE connection established" |
| Control panel EventSource errors | 100% (DNS failure) | 0% | Browser DevTools Network tab |
| OpenCode authentication warnings | 100% of pods | 0% | Minion pod logs grep for "unsecured" |

### Qualitative
- Manual validation: Real-time events visible in control panel during minion execution
- Developer experience: No need to tail pod logs to debug minions
- System confidence: Control panel accurately reflects minion state

---

## Acceptance Criteria

### Feature: OpenCode Network Binding Fix
- [ ] OpenCode server listens on `0.0.0.0:4096` instead of `127.0.0.1:4096`
- [ ] Devbox entrypoint validates `OPENCODE_SERVER_PASSWORD` env var exists before starting
- [ ] Devbox entrypoint uses `--hostname "0.0.0.0"` flag (line 80)
- [ ] Orchestrator passes `OPENCODE_SERVER_PASSWORD` env var to pod in buildEnvVars()
- [ ] Orchestrator SSE client successfully connects to pod IP addresses
- [ ] HTTP GET to `http://<pod-ip>:4096/event` returns 200 OK with authentication
- [ ] HTTP GET without credentials returns 401 Unauthorized

### Feature: Orchestrator Restart Reconnection
- [ ] On orchestrator startup (after reconciliation), query minions with: `status IN ('pending', 'running') AND opencode_password IS NOT NULL AND pod_name IS NOT NULL`
- [ ] For each matching minion, call `sseClient.Connect(ctx, m.ID, m.PodName)` to restore SSE stream
- [ ] SSE client retrieves password from connections map (stored during initial connect)
- [ ] If reconnection fails, minion continues executing (observability loss acceptable, minion completes normally)
- [ ] Reconnection logic executes in `orchestrator/cmd/orchestrator/main.go` after line 156

### Feature: Per-Minion Password Authentication
- [ ] Orchestrator generates unique UUID password per minion
- [ ] Password is stored in database BEFORE pod spawn (prevents race condition)
- [ ] Idempotent check: reuse existing password if orchestrator crashes and retries spawn
- [ ] Password is passed to pod via `OPENCODE_SERVER_PASSWORD` env var
- [ ] Orchestrator SSE client uses HTTP Basic Auth with format: `Authorization: Basic base64("opencode:<password>")`
- [ ] Passwords are stored in SSEClient connections map for reconnection after orchestrator restart
- [ ] Passwords are cleared when minion reaches any terminal status (completed/failed/terminated)
- [ ] Database migration 005 successfully adds `opencode_password TEXT` column
- [ ] All 8 DB query methods include `opencode_password` in SELECT and Scan
- [ ] SSE endpoint path bug fixed: `/events` → `/event` in `orchestrator/internal/streaming/sse.go:190`
- [ ] HTTP connection pool configured: MaxIdleConns=100, MaxConnsPerHost=50

### Feature: Control Panel SSE Migration
- [ ] New API route `/api/minions/[id]/events/stream` returns `text/event-stream` response
- [ ] API route connects to orchestrator WebSocket hub server-side
- [ ] WebSocket messages are transformed to SSE format
- [ ] SSE proxy sends heartbeat comments every 30s
- [ ] SSE proxy handles browser disconnect via abort signal
- [ ] Control panel hook replaces WebSocket with EventSource
- [ ] EventSource connects to same-origin endpoint
- [ ] Legacy `/api/ws-config` route is deleted

### Feature: Local Validation
- [ ] Go unit tests pass for new password methods
- [ ] Docker images build successfully for orchestrator, devbox, control-panel
- [ ] Manual E2E: Spawn minion, verify control panel shows events in real-time
- [ ] Manual E2E: Kill orchestrator pod, verify reconnection to active minions
- [ ] Manual E2E: Verify password cleared after minion completion
- [ ] Curl test: `curl -H "Authorization: Basic $(echo -n 'opencode:<password>' | base64)" http://<pod-ip>:4096/event` returns 200 + SSE stream
- [ ] Curl test: `curl http://<pod-ip>:4096/event` (no auth) returns 401
- [ ] TypeScript compiles without errors
- [ ] Control panel `npm run build` succeeds

---

## Technical Context

### Existing Patterns

**Password Storage**:
- `orchestrator/internal/db/minions.go` - Minion model with nullable columns for transient data (SessionID, PodName, Error)
- Pattern: Store transient credentials during execution, clear on completion

**Secret Management**:
- `infra/deployments.yaml:91-100` - Orchestrator loads secrets via `envFrom.secretRef`
- `infra/secrets.yaml` - Static secrets (DB, GitHub, Discord, LLM keys)
- Pattern: Per-pod dynamic secrets are passed as plain `env.value` (not `secretKeyRef`)

**SSE Streaming**:
- `orchestrator/internal/streaming/sse.go` - Existing SSE client with retry/backoff logic
- Connects to pod `/event` endpoint, reads SSE stream, broadcasts to WebSocket hub
- Pattern: Long-lived connection with reconnection logic

**Control Panel API Proxy**:
- `control-panel/src/app/api/minions/[id]/events/route.ts` - Existing REST endpoint that proxies orchestrator API
- `control-panel/src/lib/orchestrator.ts` - Orchestrator client using `ORCHESTRATOR_URL` env var
- Pattern: Server-side proxy to avoid CORS/DNS issues in browser

### Key Files

**Database**:
- `schema/migrations/002_create_minions_table.sql` - Minion table schema
- `orchestrator/internal/db/minions.go` - Minion model and store (8 query methods)

**Devbox**:
- `devbox/entrypoint.sh:22-28` - Environment variable configuration
- `devbox/entrypoint.sh:41-54` - validate_env() function
- `devbox/entrypoint.sh:80` - OpenCode server start command

**Control Panel**:
- `control-panel/src/hooks/use-minion-events.ts:134-261` - Replace WebSocket with EventSource
- `control-panel/src/app/api/ws-config/route.ts` - **DELETE** this file
- `control-panel/src/app/api/minions/[id]/stream/route.ts` - **CREATE** new SSE proxy route
- `control-panel/package.json` - Add `ws` npm package dependency

**Orchestrator**:
- `orchestrator/internal/spawner/spawner.go:185-240` - Minion spawn flow
- `orchestrator/internal/k8s/pods.go:91-99` - SpawnParams struct
- `orchestrator/internal/k8s/pods.go:154-276` - SpawnPod implementation
- `orchestrator/internal/k8s/pods.go:418-435` - buildEnvVars
- `orchestrator/internal/streaming/sse.go:183-221` - streamEvents

**Devbox**:
- `devbox/entrypoint.sh:22-28` - Environment variable configuration
- `devbox/entrypoint.sh:41-54` - validate_env() function
- `devbox/entrypoint.sh:80` - OpenCode server start command

**Control Panel**:
- `control-panel/src/hooks/use-minion-events.ts:134-261` - WebSocket event hook
- `control-panel/src/app/api/ws-config/route.ts` - WebSocket config endpoint (to delete)
- `control-panel/src/app/api/minions/[id]/events/route.ts` - Reference for new SSE route

### System Dependencies

**External Services**:
- OpenCode CLI server (version supporting `--hostname` flag and `OPENCODE_SERVER_PASSWORD` env var)
- OpenCode HTTP Basic Auth: username defaults to `opencode`, password from `OPENCODE_SERVER_PASSWORD`
- OpenCode SSE endpoint: `GET /event` returns `text/event-stream`

**Infrastructure**:
- Kubernetes 1.28+ (pod-to-pod networking, DNS, secrets)
- PostgreSQL 16 (for migration 005)
- Orchestrator init container runs migrations on deploy

**Package Requirements**:
- Go: `github.com/google/uuid` (already imported)
- Next.js: `ws` package for WebSocket client
- Browser: `EventSource` API (supported in all modern browsers)

### Data Model Changes

**Migration**: `005_add_opencode_password.sql`
```sql
ALTER TABLE minions ADD COLUMN opencode_password TEXT;
```

**Column Properties**:
- Nullable (existing rows have NULL, new rows get UUID)
- Transient (cleared on terminal state)
- Not indexed (low-cardinality transient data, queried by PK only)

**Lifecycle State Machine**:
1. NULL when created (status=pending)
2. Set to UUID when spawner processes minion (BEFORE pod spawn - prevents race condition)
3. Passed to pod via `OPENCODE_SERVER_PASSWORD` env var
4. Read by SSE client when connecting to pod (from DB on initial connect)
5. Stored in memory (SSEClient connections map) for reconnections after orchestrator restart
6. Cleared to NULL when status transitions to completed/failed/terminated

**Edge Cases Handled**:
- **Orchestrator crash between generate and spawn**: Password stored in DB first, survives crash
- **Orchestrator restart mid-spawn**: Idempotent check reuses existing password if non-null
- **Concurrent spawn attempts**: Existing `ErrInvalidStatusTransition` prevents race
- **SSE reconnect needs password**: Store in connections map (in-memory) during initial connect

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| OpenCode doesn't support `--hostname 0.0.0.0` | Low | Critical | **VALIDATED**: Tested locally with OpenCode server, flag works. |
| Race condition: SSE connects before password stored | Medium | High | **MITIGATED**: Store password in DB BEFORE spawning pod (not after). |
| Password visible in `kubectl get pod -o yaml` | High | Low | **ACCEPTED**: Internal cluster only. Plain env var is fine per security posture. |
| Control panel SSE proxy leaks connections on browser close | Medium | Medium | Use `req.signal.addEventListener('abort')` to detect client disconnect. |
| Migration 005 fails in init container | Low | Critical | Test migration locally with Docker Compose postgres container. |
| Orchestrator restarts mid-spawn loses password | Medium | Medium | **MITIGATED**: Password stored in DB before spawn, survives orchestrator crash. Idempotent check reuses existing password. |
| OpenCode auth fails with UUID password | Low | High | **VALIDATED**: Tested HTTP Basic Auth locally, confirmed 401 without auth, 200 with auth. |
| Orchestrator restart loses SSE connections to active minions | High | Critical | **MITIGATED**: New reconnection logic queries active minions on startup, restores SSE streams. |
| In-flight minions break during orchestrator redeploy | High | Medium | **ACCEPTED**: Per user requirement, acceptable blast radius. Document in deployment notes. |
| SSE connection limit exceeded (>50 concurrent minions) | Low | Medium | **ACCEPTED**: Per user requirement, 50 concurrent is acceptable. HTTP transport configured with MaxIdleConns=100, MaxConnsPerHost=50. |

---

## Alternatives Considered

### Alternative 1: NetworkPolicy Isolation (No Authentication)
- **Description:** Use Kubernetes NetworkPolicy to restrict OpenCode access to orchestrator pod only, skip password auth.
- **Pros:** Simpler implementation, no password management, network-layer security.
- **Cons:** NetworkPolicy already exists but isn't deployed. Doesn't address the binding issue. Weaker security model (any pod in namespace can connect).
- **Decision:** Rejected. Defense-in-depth prefers auth + network policy. Auth is required anyway for SSE.

### Alternative 2: Shared Static Password
- **Description:** Use single `OPENCODE_SERVER_PASSWORD` from Kubernetes Secret, same for all minions.
- **Pros:** Simpler (no DB column, no per-pod generation, no cleanup).
- **Cons:** Password compromise affects all minions (past and future). Credential rotation requires redeploying all pods.
- **Decision:** Rejected. Per-pod passwords limit blast radius. UUID rotation is free.

### Alternative 3: Keep WebSocket, Use Ingress for DNS
- **Description:** Deploy ingress controller, expose orchestrator WebSocket via public DNS/IP.
- **Pros:** No control panel code changes, WebSocket stays as-is.
- **Cons:** Exposes internal orchestrator to internet (security risk). Requires TLS cert management. Doesn't solve OpenCode binding issue. Adds infrastructure complexity.
- **Decision:** Rejected. SSE over same-origin is simpler and more secure.

### Alternative 4: Polling Instead of Streaming
- **Description:** Control panel polls `/api/minions/{id}/events` REST endpoint every 5s.
- **Pros:** Simple, no WebSocket/SSE complexity, already implemented.
- **Cons:** 5s latency for updates, increased API load, poor UX for real-time monitoring.
- **Decision:** Rejected. Real-time streaming is a core requirement for observability.

---

## Interface Specifications

### Database Schema

**New Column**: `minions.opencode_password TEXT`
- Nullable (NULL for existing rows and after clear)
- Stores UUID v4 (36 characters, format: `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx`)
- Lifecycle: NULL → UUID (on spawn) → NULL (on terminal state)

**New Methods**:
```go
// Store password before spawning pod
func (s *MinionStore) StorePassword(ctx context.Context, id uuid.UUID, password string) error

// Clear password on terminal state
func (s *MinionStore) ClearPassword(ctx context.Context, id uuid.UUID) error
```

### OpenCode Server

**Listen Address**: `0.0.0.0:4096` (all interfaces)
**Authentication**: HTTP Basic Auth
- Username: `opencode` (OpenCode default)
- Password: `OPENCODE_SERVER_PASSWORD` env var
- Header: `Authorization: Basic base64("opencode:<password>")`

**Endpoint**: `GET /event` (singular, NOT `/events`)
- Returns: `Content-Type: text/event-stream`
- Auth required: 401 if missing/wrong credentials
- Stream format: Standard SSE (`event: <type>\ndata: <json>\n\n`)
- **Known Bug**: Current code at `orchestrator/internal/streaming/sse.go:190` uses `/events` (plural) - must fix to `/event`

### Control Panel API

**New Route**: `GET /api/minions/[id]/events/stream`
- Returns: `Content-Type: text/event-stream`
- Headers: `Cache-Control: no-cache, no-transform`, `Connection: keep-alive`, `X-Accel-Buffering: no`
- Behavior:
  - Connects to orchestrator WebSocket server-side
  - Transforms WebSocket messages to SSE format
  - Sends `: heartbeat\n\n` every 30s
  - Closes WebSocket when browser disconnects (`req.signal.addEventListener('abort')`)
  - Emits `event: error\ndata: {...}\n\n` on upstream failure

**Deleted Route**: `GET /api/ws-config` (legacy)

### Frontend Hook

**Updated**: `use-minion-events.ts`
- Replace `WebSocket` with `EventSource`
- Remove `fetchWSConfig()` dependency
- Use same-origin URL: `/api/minions/${minionId}/events/stream`
- Handle `eventSource.onmessage`, `onerror`, `onopen`
- Cleanup via `eventSource.close()`

---

## Deployment Strategy

**Impact**: In-flight minions (spawned before orchestrator redeploy) will lose observability during the deployment. This is acceptable per user requirements.

**Behavior**:
- Old minions: Continue executing, complete normally, but no SSE streaming during deployment window
- New minions: Full observability immediately after orchestrator redeploy
- Orchestrator restart: Reconnects to all active minions with stored passwords

**Deployment Steps**:
1. Run migration 005 (adds `opencode_password` column, no data migration needed)
2. Redeploy orchestrator (new SSE logic, reconnection on startup)
3. Redeploy devbox image (OpenCode binding + password validation)
4. Redeploy control panel (SSE proxy route)
5. Test: Spawn new minion, verify control panel shows events
6. Test: Kill orchestrator pod, verify reconnection

---

## Documentation Requirements

- [ ] Update AGENTS.md with orchestrator restart reconnection behavior
- [ ] Document password lifecycle state machine in code comments
- [ ] Add troubleshooting guide for SSE connection failures (401 auth, connection refused)
- [ ] Update control panel README with EventSource architecture
- [ ] Add deployment notes: in-flight minions lose observability during redeploy (acceptable)

---

## Implementation Estimate

**Total Effort**: 9.5 hours (1.2 work days)

**Breakdown by Phase**:
1. Database (15m): Migration + methods
2. Orchestrator Core (2h): Fix SSE endpoint bug, add auth, connection limits
3. Spawner Logic (1h): Password generation before spawn
4. Orchestrator Startup Reconnection (30m): Query active minions, reconnect SSE
5. Password Cleanup Hooks (30m): Clear password in callback handler + watchdog
6. Devbox (30m): Hostname flag + password validation
7. Control Panel (2h): SSE proxy route + EventSource hook + delete ws-config
8. PRD Updates (30m): Document gaps and decisions
9. Testing & Validation (2h): Unit tests + Docker builds + manual E2E

**File Changes**: 11 files (1 new migration, 1 new API route, 8 edits, 1 delete)

---

## Appendix

### Glossary

- **SSE (Server-Sent Events)**: HTTP-based unidirectional streaming protocol. Browser initiates with EventSource API, server pushes events.
- **OpenCode**: AI coding agent CLI/server. Runs in minion pods, exposes HTTP API for task execution and event streaming.
- **Minion**: Ephemeral Kubernetes pod running OpenCode to execute a single coding task.
- **Orchestrator**: Go service managing minion lifecycle (spawn, stream, terminate, callbacks).
- **Control Panel**: Next.js web UI for monitoring minion status and events.
- **Pod IP**: Kubernetes-assigned IP address for pod-to-pod communication within cluster network.

### References

- OpenCode Server Docs: https://opencode.ai/docs/server
- OpenCode Authentication: `OPENCODE_SERVER_PASSWORD` env var for HTTP Basic Auth
- OpenCode SSE Endpoint: `GET /event` (validated locally - singular not plural)
- Existing Architecture: `minions/AGENTS.md`
- Database Schema: `schema/migrations/002_create_minions_table.sql`
- Orchestrator SSE Implementation: `orchestrator/internal/streaming/sse.go`

---

## Implementation Plan

### Phase 1: Database (15m)
1. Create `schema/migrations/005_add_opencode_password.sql`
2. Add `OpencodePassword string` field to Minion struct in `orchestrator/internal/db/minions.go`
3. Implement `StorePassword(ctx, id, password) error` method
4. Implement `ClearPassword(ctx, id) error` method
5. Update all 8 existing query methods to SELECT and Scan `opencode_password` column

### Phase 2: Orchestrator Core (2h)
1. Add `OpencodePassword string` to SpawnParams struct in `orchestrator/internal/k8s/pods.go:91-99`
2. **Fix critical bug**: `orchestrator/internal/streaming/sse.go:190` change `/events` → `/event`
3. Add `password string` parameter to `Connect()`, `connectWithRetry()`, `streamEvents()` methods
4. Change `SSEClient.connections` from `map[uuid.UUID]context.CancelFunc` to `map[uuid.UUID]*connection` where `connection` holds `{cancel, password, podName}`
5. Add HTTP Basic Auth header in `streamEvents()` after line 202: `req.Header.Set("Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:" + password)))`
6. Update `httpClient` in `NewSSEClient()` (line 96-98) with transport config: `MaxIdleConns: 100, MaxConnsPerHost: 50`
7. Add `import "encoding/base64"` to sse.go

### Phase 3: Spawner Logic (1h)
1. Modify `orchestrator/internal/spawner/spawner.go` processMinion() before line 185
2. Add password check: if `m.OpencodePassword != ""` reuse, else generate UUID and call `StorePassword()`
3. Pass `OpencodePassword` to SpawnParams
4. Update MinionUpdater interface to include `StorePassword()` method

### Phase 4: Orchestrator Startup Reconnection (30m)
1. Add reconnection logic in `orchestrator/cmd/orchestrator/main.go` after reconciliation (~line 156)
2. Query: `minionStore.ListByStatuses(ctx, []db.MinionStatus{db.StatusRunning, db.StatusPending})`
3. For each minion where `OpencodePassword != "" && PodName != ""`: call `sseClient.Connect(ctx, m.ID, m.PodName)`

### Phase 5: Password Cleanup Hooks (30m)
1. Add `ClearPassword()` call in `orchestrator/internal/api/minions.go` HandleCallback() when status is completed/failed
2. Add `ClearPassword()` call in `orchestrator/internal/watchdog/watchdog.go` when marking minion as failed

### Phase 6: Devbox (30m)
1. Update `devbox/entrypoint.sh` line 41-54: add `OPENCODE_SERVER_PASSWORD` to validate_env() required vars
2. Change line 80: `--hostname "127.0.0.1"` → `--hostname "0.0.0.0"`
3. Update `orchestrator/internal/k8s/pods.go` buildEnvVars() to pass `OPENCODE_SERVER_PASSWORD` env var to pod

### Phase 7: Control Panel (2h)
1. Run `npm install ws` in control-panel directory
2. Create `control-panel/src/app/api/minions/[id]/stream/route.ts` (SSE proxy route)
3. Modify `control-panel/src/hooks/use-minion-events.ts` lines 134-261: replace WebSocket with EventSource
4. Delete `control-panel/src/app/api/ws-config/route.ts`

### Phase 8: Testing & Validation (2h)
1. Go unit tests for password methods
2. Build Docker images (orchestrator, devbox, control-panel)
3. Deploy to cluster
4. Manual E2E: spawn minion, verify control panel shows events
5. Manual E2E: kill orchestrator, verify reconnection
6. Manual E2E: verify password cleared after completion
7. Curl tests: auth/no-auth to minion pod
