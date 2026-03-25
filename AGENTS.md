# Minions Repository Agent Guide

## Overview

Minions is a Discord bot-driven AI agent orchestrator that spawns ephemeral Kubernetes pods to execute coding tasks.

## Repository Structure

```
minions/
├── orchestrator/     # Go service - manages Kubernetes pods and task lifecycle
├── discord-bot/      # Go service - Discord integration
├── control-panel/    # Next.js web UI for monitoring
├── devbox/           # Container image for running tasks
├── infra/            # Kubernetes manifests
├── release-configs/  # Semantic-release configs per service
└── schema/           # Shared schemas/types
```

## Skills

### Git Workflow (Required for Commits)

**IMPORTANT**: Before making any commits or submitting changes, load the git-workflow skill:

```
Load skill: git-workflow
```

This skill defines:
- Conventional commit format (required for semantic-release)
- Branch-based development (no direct pushes to main)
- PR submission workflow

Key rules:
1. **Never push directly to main** - Always create a branch and PR
2. **Use conventional commits** - `feat:`, `fix:`, `chore:`, etc.
3. **Include disclosure** - All PRs must include the AI disclosure statement

## Deployment Policy

**CRITICAL**: Never deploy directly to Kubernetes. All Kubernetes deployments are handled by a human.

- Do not run `kubectl apply`, `kubectl create`, `kubectl patch`, or any other commands that modify cluster state
- You may read cluster state with `kubectl get`, `kubectl describe`, etc. for debugging purposes
- You may modify Kubernetes manifests in `infra/` but never apply them
- If deployment is needed, inform the user and let them handle it

## CI/CD

- **Commitlint**: Validates conventional commits on PRs
- **Semantic-release**: Creates version tags on merge to main (per-service)
- **Docker build**: Builds images on version tags (`*-v*`)

Images are published to `ghcr.io/imdevinc/minions/<service>`.

## Development

### Building Services

```bash
# Go services (orchestrator, discord-bot)
cd <service> && go build ./...

# Control panel (Next.js)
cd control-panel && npm install && npm run build

# Devbox image
docker build -t devbox ./devbox
```

### Testing

```bash
# Go services
cd <service> && go test ./...

# Control panel
cd control-panel && npm test
```

## Monorepo Versioning

Each service is versioned independently:
- Tags: `<service>-v<semver>` (e.g., `orchestrator-v1.2.0`)
- Releases only trigger for services with changes
- `release-configs/<service>.js` contains per-service semantic-release config

## Architecture

### Minion Observability & Security

The orchestrator streams real-time events from minion pods to the control panel using Server-Sent Events (SSE) over unsecured HTTP (trusted pod network).

#### Orchestrator Restart Behavior

On restart, the orchestrator reconnects to active minions:
1. Query DB for minions with status=running or status=pending
2. Filter where `pod_name != NULL`
3. For each eligible minion, call `SSEClient.Connect(ctx, id, podName)`
4. Connect is fire-and-forget (spawns goroutine with internal retry logic)
5. Reconnection failures are logged but don't fail startup

**Implication:** Active minions survive orchestrator restarts without losing observability.

#### Control Panel SSE Proxy

The control panel cannot directly connect to minion pods (different namespaces, no ingress). Architecture:

```
Browser ──[EventSource]──> Next.js API Route ──[WebSocket]──> Orchestrator ──[HTTP]──> Minion Pod
         /api/minions/[id]/events/stream      /api/minions/[id]/stream          :4096/event
```

**Why Server-Side Proxy:**
- Keeps orchestrator internal_api_token secret (never sent to browser)
- Avoids CORS issues
- Handles WebSocket → SSE transformation
- Survives orchestrator restarts (EventSource auto-reconnects)

**Implementation:** control-panel/src/app/api/minions/[id]/events/stream/route.ts

#### SSE Connection Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| **Connection refused** | OpenCode not listening on 0.0.0.0 | Check devbox/entrypoint.sh hostname flag, verify OpenCode started successfully |
| **Connection timeout** | Pod not ready or network issue | Check pod status with `kubectl get pod`, verify pod IP reachable |
| **SSE reconnection loop** | Orchestrator crashing | Check orchestrator logs for panic, verify minion status is not terminal |

**Debug Commands:**
```bash
# Check pod status
kubectl get pod <pod-name> -n minions

# Check OpenCode server listening
kubectl exec <pod-name> -n minions -- netstat -tlnp | grep 4096

# Check orchestrator SSE connection logs
kubectl logs -n minions <orchestrator-pod> | grep "SSE connection established\|Failed to connect SSE"
```

#### Token and Cost Tracking

The orchestrator tracks granular token usage and API costs for each minion task.

**Data Model:**

The database stores 6 token fields per minion:
- `input_tokens`: User prompt tokens
- `output_tokens`: Assistant response tokens
- `reasoning_tokens`: Extended thinking tokens (Claude models)
- `cache_read_tokens`: Prompt cache hits
- `cache_write_tokens`: Prompt cache writes
- `cost_usd`: Total USD cost for all API calls

**Display Totals:**

Backend combines granular fields for UI display:
- **Input Total** = `input_tokens` + `cache_read_tokens`
- **Output Total** = `output_tokens` + `reasoning_tokens` + `cache_write_tokens`

This combining happens in two places:
1. **Stats aggregation** (orchestrator/internal/db/minions.go:GetStats) - SQL SUM() queries combine at query time
2. **API responses** (orchestrator/internal/api/minions.go:HandleGet) - In-memory combination before JSON serialization

**SSE Extraction:**

Token and cost data is extracted from OpenCode SSE events via path: `content.info`

```json
{
  "type": "message.updated",
  "content": {
    "info": {
      "cost": 0.02954525,
      "tokens": {
        "input": 1763,
        "output": 929,
        "reasoning": 793,
        "cache": {
          "read": 13440,
          "write": 0
        }
      }
    }
  }
}
```

Implementation: orchestrator/internal/streaming/sse.go:extractTokenUsage() uses type-safe assertions with graceful nil handling.

**Real-time Updates:**

The control panel minion detail page polls `GET /api/minions/:id` every 3 seconds while minion status is `running` or `pending`. Polling stops automatically when terminal status is reached. Cost displays with 5 decimal places, tokens display with thousands separators.

#### Deployment Notes

**Observability Loss During Redeploy:**
- When orchestrator pod is replaced, in-flight SSE connections are dropped
- Reconnection happens automatically after new orchestrator starts (see restart behavior above)
- Minions continue executing during orchestrator downtime (no task interruption)
- Control panel shows "disconnected" status briefly, then reconnects via EventSource retry

**Acceptable Blast Radius:**
- ~30-60 seconds of lost observability during orchestrator rolling update
- No minion task failures (execution is independent)
- No user intervention required (automatic reconnection)
