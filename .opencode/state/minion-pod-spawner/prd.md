# PRD: Minion Pod Spawner

**Date:** 2026-03-22

---

## Problem Statement

### What problem are we solving?

Minions are created in the database but their Kubernetes pods never actually spin up. The discord-bot creates a minion record (status=`pending`) and tells the user "Your minion is being spawned...", but nothing happens. The minion sits in `pending` status forever.

**Root cause:** The codebase assumes an "orchestrator's pod spawner will pick it up" (see `discord-bot/internal/handler/message.go:186-187`), but this spawner loop was never implemented. The orchestrator only has:
- A **reconciler** that runs at startup (cleanup only)
- A **watchdog** that monitors idle/failed minions (alerts only)
- No component that polls for pending minions and spawns pods

**User impact:** Complete feature failure. Users cannot run any minion tasks.

### Why now?

The system is non-functional without this component. All other pieces are in place:
- Discord bot creates minions ✓
- k8s client can spawn pods ✓ (PR #8 wired up real client)
- Devbox image exists ✓
- SSE streaming infrastructure exists ✓

This is the missing link that connects minion creation to pod execution.

### Who is affected?

- **Primary users:** Discord users invoking `@minion` commands. They see "spawning..." but nothing happens.
- **Secondary users:** Control panel users monitoring minions. They see minions stuck in `pending` status.

---

## Proposed Solution

### Overview

Add a background `spawner` component to the orchestrator that continuously polls for minions in `pending` status, spawns Kubernetes pods for them, waits for pod readiness, updates the minion status to `running`, and initiates SSE event streaming from the pod.

The spawner follows the existing `watchdog` pattern: a goroutine with a ticker that runs periodic checks, with clean shutdown via stop channel.

---

## End State

When this PRD is complete, the following will be true:

- [ ] Minions in `pending` status are automatically picked up by the spawner
- [ ] Spawner generates GitHub App installation tokens for repo access
- [ ] Spawner calls `SpawnPodWithRetry()` to create pods with exponential backoff
- [ ] Spawner waits for pod readiness before marking minion as `running`
- [ ] Minion status transitions: `pending` → `running` with `pod_name` set
- [ ] SSE event streaming connects to the pod after spawn
- [ ] Spawn failures mark the minion as `failed` with error message
- [ ] Spawner integrates cleanly with existing startup/shutdown lifecycle
- [ ] Structured logs capture spawn attempts, successes, and failures
- [ ] Tests cover the spawner logic

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Minion spawn success rate | 0% (broken) | >95% | Logs: "minion spawned successfully" / total attempts |
| Time from pending to running | N/A | <30 seconds | Log timestamps: created_at to started_at |
| Spawn failure rate | N/A | <5% | Logs: spawn failures / total attempts |

### Qualitative

- Users see their minions actually start working after invoking commands
- Control panel shows minions transitioning through expected states
- No orphaned pods or stuck minions under normal operation

---

## Acceptance Criteria

### Feature: Spawner Loop

- [ ] Spawner runs as a background goroutine started in `main.go`
- [ ] Polls for `pending` minions every 5 seconds
- [ ] Processes minions sequentially (one at a time)
- [ ] Stops cleanly on context cancellation or stop signal
- [ ] Logs startup, check intervals, and shutdown

### Feature: Pod Spawning

- [ ] Generates GitHub App installation token for the minion's repo
- [ ] Calls `SpawnPodWithRetry()` with correct `SpawnParams`
- [ ] Waits for pod readiness via `WaitForPodReady()`
- [ ] Passes `OrchestratorURL` and `InternalAPIToken` to pod

### Feature: Status Transitions

- [ ] New `MarkRunning(id, podName)` method on `MinionStore`
- [ ] Updates status to `running`, sets `pod_name`, `started_at`, `last_activity_at`
- [ ] Only transitions from `pending` status (not `awaiting_clarification`)

### Feature: Error Handling

- [ ] GitHub token failures mark minion as failed with clear error
- [ ] Pod spawn failures (after retries exhausted) mark minion as failed
- [ ] Pod readiness timeout marks minion as failed
- [ ] `MarkRunning` DB failures log error but don't crash spawner

### Feature: SSE Streaming

- [ ] After successful spawn, initiates SSE connection to pod
- [ ] Uses existing `streaming.SSEClient` infrastructure
- [ ] SSE client handles reconnection internally

---

## Technical Context

### Existing Patterns

- **Watchdog pattern:** `orchestrator/internal/watchdog/watchdog.go` - Background loop with ticker, stop channel, graceful shutdown. Spawner should mirror this structure exactly.
- **Pod management:** `orchestrator/internal/k8s/pods.go` - `SpawnPodWithRetry()`, `WaitForPodReady()` methods already implemented with retry logic.
- **GitHub tokens:** `orchestrator/internal/github/` - Token manager for generating installation tokens.

### Key Files

- `orchestrator/cmd/orchestrator/main.go` - Wire up spawner similar to watchdog
- `orchestrator/internal/db/minions.go` - Add `MarkRunning()` method
- `orchestrator/internal/k8s/pods.go` - `SpawnParams` struct defines required parameters
- `orchestrator/internal/streaming/` - SSE client for event streaming
- `infra/deployments.yaml` - Environment variables available to orchestrator

### System Dependencies

- **Kubernetes API:** In-cluster client for pod operations
- **PostgreSQL:** Minion state storage
- **GitHub API:** Installation token generation via GitHub App

### Data Model Changes

No schema changes required. Uses existing columns:
- `minions.status` - Transition from `pending` to `running`
- `minions.pod_name` - Set when pod is spawned
- `minions.started_at` - Set when transitioning to `running`
- `minions.last_activity_at` - Updated on status change

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Crash between pod spawn and MarkRunning | Low | Med | Reconciler detects orphaned pods on restart; could enhance to recover pending+pod-exists state |
| GitHub token generation fails | Low | High | Fail fast, mark minion as failed with clear error message |
| Pod never becomes ready (bad image, OOM) | Med | Med | `WaitForPodReady` has timeout (5 min); pod deleted and minion marked failed |
| Spawner overwhelms cluster with pods | Low | Med | Sequential processing (one at a time) prevents thundering herd |
| SSE connection fails on first attempt | High | Low | SSE client has built-in reconnection with exponential backoff |

---

## Alternatives Considered

### Alternative 1: Spawn in API Handler

- **Description:** Spawn pod synchronously in POST /api/minions handler
- **Pros:** Simpler, no background loop needed
- **Cons:** Spawning takes 10-30s, would timeout Discord webhook; blocks API
- **Decision:** Rejected. Async spawner is correct pattern for long-running operations.

### Alternative 2: Event-Driven with Message Queue

- **Description:** Publish minion creation to queue, spawner consumes events
- **Pros:** Decoupled, scales horizontally, at-least-once delivery
- **Cons:** Adds infrastructure complexity (Redis/RabbitMQ/NATS); overkill for single-replica deployment
- **Decision:** Rejected. Polling is simpler and sufficient for current scale.

### Alternative 3: Kubernetes Operator Pattern

- **Description:** Watch minion CRDs, reconcile to desired state
- **Pros:** Native k8s pattern, handles all edge cases
- **Cons:** Requires CRD definitions, operator SDK, significant complexity increase
- **Decision:** Rejected. We're not managing k8s resources as CRDs; minions are in PostgreSQL.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Horizontal scaling / locking** - Current deployment is single replica. Add `FOR UPDATE SKIP LOCKED` only if we need multiple replicas.
- **Concurrent spawning** - Process one minion at a time. Add semaphore-based concurrency only if we see >5 pending minions regularly.
- **Prometheus metrics** - Structured logs are sufficient for v1. Add metrics if we need dashboards/alerts.
- **Per-minion retry tracking** - Spawner marks failed after `SpawnPodWithRetry` exhausts retries. Add DB retry counter only if we see transient failures that should be retried later.
- **Priority queue** - FIFO ordering by `created_at` is sufficient. Add priority column only if we have VIP users.

---

## Interface Specifications

### Internal API (Spawner → MinionStore)

```go
// New method to add
func (s *MinionStore) MarkRunning(ctx context.Context, id uuid.UUID, podName string) error

// Updates:
//   status = 'running'
//   pod_name = podName
//   started_at = NOW()
//   last_activity_at = NOW()
// WHERE id = id AND status = 'pending'
```

### Environment Variables Required

```
GITHUB_APP_ID          - Already in minions-github-app secret
GITHUB_APP_PRIVATE_KEY - Already in minions-github-app secret
INTERNAL_API_TOKEN     - Already in minions-internal-api secret
ORCHESTRATOR_URL       - Needs to be added (default: http://orchestrator.minions.svc.cluster.local:8080)
```

---

## Documentation Requirements

- [ ] Update `docs/architecture.md` to mention spawner component
- [ ] Add spawner to system diagram showing orchestrator internals

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| Should reconciler handle "pending minion + running pod" edge case? | - | - | Open (nice-to-have, not blocking) |

---

## Appendix

### Glossary

- **Minion:** A task execution unit representing one coding task
- **Devbox:** The container image that runs OpenCode to execute tasks
- **Spawner:** The new component that creates pods for pending minions
- **SSE:** Server-Sent Events, used for streaming logs from pods

### References

- [Architecture diagram](docs/architecture.md) - Shows minion lifecycle flow
- [PR #8](https://github.com/ImDevinC/minions/pull/8) - Wired up real k8s client
- Oracle review in conversation - Detailed implementation recommendations
