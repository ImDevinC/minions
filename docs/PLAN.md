# Minions System - Implementation Plan

> Inspired by [Stripe's Minions](https://stripe.dev/blog/minions-stripes-one-shot-end-to-end-coding-agents) - one-shot, end-to-end coding agents.

## Critical Implementation Notes

These fixes address gaps identified during architecture review:

### Security (Critical)
1. **Pod Security Context**: Devbox pods must run with restricted privileges to prevent container escape.
2. **API Authentication**: Orchestrator API requires shared secret auth from Discord bot and control panel.
3. **GitHub App Token Refresh**: Installation tokens expire after 1 hour. Use `ghinstallation` library with automatic refresh.

### Reliability (High)
4. **Async Clarification**: Discord clarification must be non-blocking (state machine, not goroutine wait).
5. **Duplicate Minion Prevention**: Advisory lock + dedup check to prevent race conditions.
6. **Pod Creation Retry**: Exponential backoff on K8s API failures.
7. **SSE Reconnection**: Auto-reconnect to pod event stream with backoff.
8. **Clarification Timeout**: 24h TTL for pending clarifications.
9. **Rate Limiting**: Max 10 minions/hour per user, max 3 concurrent.

### Infrastructure
10. **Pod-to-Orchestrator Network**: Explicit network policy required for callback connectivity.
11. **Orphan Pod Cleanup**: On orchestrator startup, reconcile DB state with K8s pods.
12. **Event Partitioning**: `minion_events` table needs time-based partitioning for scale.
13. **Pod Readiness Probe**: Wait for OpenCode `/global/health` before marking pod ready.

### Code Quality
14. **JSON Escaping**: Use `jq` in entrypoint.sh to avoid shell injection.
15. **Structured Logging**: Use `log/slog` for all Go services.
16. **Cost Calculation**: Pricing table for token→USD conversion.
17. **Graceful Shutdown**: Handle SIGTERM, drain connections, close DB pool.

## Overview

A system that allows users to chat with a Discord bot, specify a repository, and have an LLM agent autonomously implement the task and create a pull request. No user interaction required after the initial command.

### Key Features

- **Discord Bot**: `@minion --repo Owner/Repo <task>` triggers agent
- **One-round clarification**: Agent can ask ONE follow-up question before starting
- **Isolated execution**: Each task runs in a fresh Kubernetes pod
- **Live observability**: Control panel shows real-time agent activity
- **Immediate termination**: Kill button stops work and notifies Discord
- **Cost tracking**: Token usage and estimated costs per minion

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                 MINIONS SYSTEM                                  │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│   Discord                                                                       │
│   ┌────────────┐     ┌─────────────────────────────────────────────────────┐   │
│   │   @minion  │────▶│              Orchestrator (Go)                      │   │
│   │   --repo   │     │  • REST API                                         │   │
│   │   <task>   │     │  • K8s pod lifecycle                                │   │
│   └────────────┘     │  • WebSocket hub for log streaming                  │   │
│         ▲            │  • Token/cost tracking                              │   │
│         │            └─────────────────────────────────────────────────────┘   │
│         │                              │                                        │
│         │              ┌───────────────┼───────────────┐                        │
│         │              ▼               ▼               ▼                        │
│         │         ┌─────────┐    ┌──────────┐    ┌──────────┐                   │
│         │         │ Pod #1  │    │ Pod #2   │    │ Pod #N   │                   │
│         │         │         │    │          │    │          │                   │
│         │         │opencode │    │opencode  │    │opencode  │                   │
│         │         │ serve   │───▶│ serve    │    │ serve    │                   │
│         │         │  + run  │    │  + run   │    │  + run   │                   │
│         │         └─────────┘    └──────────┘    └──────────┘                   │
│         │              │                                                        │
│         └──────────────┴──── PR URL / termination ─────────────────────────────│
│                                                                                 │
│   ┌────────────────────────────────────────────────────────────────────────┐   │
│   │                    Control Panel (Next.js + React)                      │   │
│   │  • Discord OAuth login                                                  │   │
│   │  • List all minions (any authed user sees all)                          │   │
│   │  • Click minion → detail page with live logs                            │   │
│   │  • Terminate button → K8s pod delete + Discord reply                    │   │
│   │  • Cost tracking display (per-minion + total)                           │   │
│   └────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
│                         ┌─────────────────────┐                                 │
│                         │     PostgreSQL      │                                 │
│                         │  (via env vars)     │                                 │
│                         └─────────────────────┘                                 │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## Tech Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| Devbox Image | Docker + OpenCode | Isolated agent execution environment |
| Orchestrator | Go | API, K8s lifecycle, event streaming, watchdog |
| Discord Bot | Go | Command parsing, clarification, notifications |
| Control Panel | Next.js + React | Dashboard, live logs, termination |
| Database | PostgreSQL | State, events, cost tracking |
| Agent | OpenCode | LLM-powered coding agent |

---

## Project Structure

```
ImDevinC/minions/
├── README.md
├── docs/
│   ├── PLAN.md                     # This file
│   ├── setup/
│   │   ├── github-app.md           # GitHub App setup guide
│   │   ├── discord-app.md          # Discord Application setup guide
│   │   └── kubernetes.md           # K8s requirements & manifests
│   └── architecture.md             # System overview
├── schema/
│   └── migrations/
│       └── 001_initial.sql
├── devbox/
│   ├── Dockerfile
│   ├── entrypoint.sh
│   └── config/
│       └── opencode.json           # Default OpenCode config
├── orchestrator/
│   ├── cmd/
│   │   └── orchestrator/
│   │       └── main.go
│   ├── internal/
│   │   ├── api/
│   │   │   ├── router.go
│   │   │   ├── minions.go
│   │   │   └── stats.go
│   │   ├── db/
│   │   │   ├── db.go
│   │   │   ├── minions.go
│   │   │   └── events.go
│   │   ├── k8s/
│   │   │   └── pods.go
│   │   ├── streaming/
│   │   │   ├── sse.go              # SSE client for OpenCode
│   │   │   └── hub.go              # WebSocket broadcast hub
│   │   ├── watchdog/
│   │   │   └── watchdog.go
│   │   └── discord/
│   │       └── webhook.go
│   ├── pkg/
│   │   └── opencode/
│   │       └── client.go           # OpenCode HTTP API client
│   ├── go.mod
│   └── go.sum
├── discord-bot/
│   ├── cmd/
│   │   └── bot/
│   │       └── main.go
│   ├── internal/
│   │   ├── bot/
│   │   │   └── bot.go
│   │   ├── commands/
│   │   │   └── minion.go
│   │   ├── clarify/
│   │   │   └── clarify.go
│   │   └── orchestrator/
│   │       └── client.go
│   ├── go.mod
│   └── go.sum
├── control-panel/
│   ├── app/
│   │   ├── layout.tsx
│   │   ├── page.tsx
│   │   ├── minions/
│   │   │   └── [id]/
│   │   │       └── page.tsx
│   │   ├── stats/
│   │   │   └── page.tsx
│   │   └── api/
│   │       └── auth/
│   │           └── [...nextauth]/
│   │               └── route.ts
│   ├── components/
│   │   ├── ui/                     # shadcn components
│   │   ├── minion-list.tsx
│   │   ├── minion-card.tsx
│   │   ├── event-log.tsx
│   │   ├── terminate-button.tsx
│   │   └── cost-display.tsx
│   ├── lib/
│   │   ├── api.ts
│   │   ├── websocket.ts
│   │   └── auth.ts
│   ├── package.json
│   ├── tailwind.config.ts
│   ├── next.config.js
│   └── tsconfig.json
└── .github/
    └── workflows/
        └── ci.yml                  # Build/test workflow
```

---

## Database Schema

```sql
-- Users (Discord OAuth)
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    discord_id TEXT UNIQUE NOT NULL,
    discord_username TEXT NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Minion runs
CREATE TABLE minions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'terminated')),
    repo TEXT NOT NULL,
    task TEXT NOT NULL,
    clarification_state TEXT NOT NULL DEFAULT 'none'
        CHECK (clarification_state IN ('none', 'pending', 'awaiting_user', 'ready')),
    clarification_question TEXT,
    clarification_answer TEXT,
    clarification_message_id TEXT,
    model TEXT NOT NULL DEFAULT 'anthropic/claude-sonnet-4-5',
    clarification_model TEXT,
    discord_message_id TEXT NOT NULL,
    discord_channel_id TEXT NOT NULL,
    discord_user_id TEXT NOT NULL,
    pod_name TEXT,
    pod_ip TEXT,
    session_id TEXT,
    pr_url TEXT,
    error_message TEXT,
    terminated_by_discord_id TEXT,
    input_tokens BIGINT DEFAULT 0,
    output_tokens BIGINT DEFAULT 0,
    estimated_cost_usd DECIMAL(10,6) DEFAULT 0,
    last_activity_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

-- OpenCode event log
CREATE TABLE minion_events (
    id BIGSERIAL PRIMARY KEY,
    minion_id UUID NOT NULL REFERENCES minions(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ DEFAULT NOW(),
    event_type TEXT NOT NULL,
    content JSONB NOT NULL
);

-- Indexes
CREATE INDEX idx_minions_status ON minions(status);
CREATE INDEX idx_minions_running_activity ON minions(last_activity_at)
    WHERE status = 'running';
CREATE INDEX idx_minions_clarification_msg ON minions(clarification_message_id)
    WHERE clarification_state = 'awaiting_user';
CREATE INDEX idx_minion_events_minion ON minion_events(minion_id, timestamp);

-- Partitioning for minion_events (by month)
-- In production, use pg_partman or manual partition management
-- CREATE TABLE minion_events (
--     ...
-- ) PARTITION BY RANGE (timestamp);
```

### Cost Pricing Table

Used by orchestrator to calculate `estimated_cost_usd` from token counts.

```go
// pkg/cost/pricing.go
var ModelPricing = map[string]struct {
    InputPer1M  float64 // USD per 1M input tokens
    OutputPer1M float64 // USD per 1M output tokens
}{
    "anthropic/claude-opus-4-5":   {InputPer1M: 15.00, OutputPer1M: 75.00},
    "anthropic/claude-sonnet-4-5": {InputPer1M: 3.00, OutputPer1M: 15.00},
    "anthropic/claude-haiku-4-5":  {InputPer1M: 0.25, OutputPer1M: 1.25},
    "openai/gpt-4o":               {InputPer1M: 2.50, OutputPer1M: 10.00},
    "openai/gpt-4o-mini":          {InputPer1M: 0.15, OutputPer1M: 0.60},
}

func CalculateCost(model string, inputTokens, outputTokens int64) float64 {
    pricing, ok := ModelPricing[model]
    if !ok {
        return 0 // Unknown model, no cost estimate
    }
    inputCost := float64(inputTokens) / 1_000_000 * pricing.InputPer1M
    outputCost := float64(outputTokens) / 1_000_000 * pricing.OutputPer1M
    return inputCost + outputCost
}
```

---

## Phase 1: Foundation

**Deliverables:**
- `README.md` with project overview
- `docs/architecture.md` with system diagram
- `schema/migrations/001_initial.sql`

---

## Phase 2: Devbox Image

**Deliverables:**
- `devbox/Dockerfile`
- `devbox/entrypoint.sh`
- `devbox/config/opencode.json`

### Dockerfile

```dockerfile
FROM golang:1.22-bookworm

# System dependencies
RUN apt-get update && apt-get install -y \
    git \
    curl \
    gnupg \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | gpg --dearmor -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Install OpenCode
RUN curl -fsSL https://opencode.ai/install | bash

# Create non-root user
RUN useradd -m -s /bin/bash minion
USER minion
WORKDIR /home/minion

# Copy default config
COPY --chown=minion:minion config/opencode.json /home/minion/.config/opencode/opencode.json

# Copy entrypoint
COPY --chown=minion:minion entrypoint.sh /home/minion/entrypoint.sh

EXPOSE 4096

ENTRYPOINT ["/home/minion/entrypoint.sh"]
```

### entrypoint.sh

```bash
#!/usr/bin/env bash
set -euo pipefail

# Required env vars
: "${REPO:?REPO is required}"
: "${TASK:?TASK is required}"
: "${CALLBACK_URL:?CALLBACK_URL is required}"
: "${MINION_ID:?MINION_ID is required}"
: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"

# Callback helper with retry logic
send_callback() {
    local payload="$1"
    local max_attempts=5
    local attempt=1
    local backoff=1
    
    while [ $attempt -le $max_attempts ]; do
        if curl -f -s -X POST "${CALLBACK_URL}" \
            -H "Content-Type: application/json" \
            -d "$payload"; then
            return 0
        fi
        echo "Callback failed (attempt $attempt/$max_attempts), retrying in ${backoff}s..."
        sleep $backoff
        attempt=$((attempt + 1))
        backoff=$((backoff * 2))
    done
    
    echo "ERROR: Callback failed after $max_attempts attempts"
    return 1
}

# Optional: override config via env
if [[ -n "${OPENCODE_CONFIG_CONTENT:-}" ]]; then
    echo "$OPENCODE_CONFIG_CONTENT" > ~/.config/opencode/opencode.json
fi

# gh CLI automatically uses GITHUB_TOKEN env var (no need to pipe/echo)
# https://cli.github.com/manual/gh_help_environment

# Clone repository (validates GitHub App has access)
if ! git clone "https://github.com/${REPO}.git" /home/minion/workspace 2>&1; then
    send_callback "$(jq -n --arg id "$MINION_ID" --arg err "Failed to clone repo. Check GitHub App has access to ${REPO}" \
        '{minion_id: $id, status: "failed", error: $err}')"
    exit 1
fi
cd /home/minion/workspace

# Start OpenCode server in background
opencode serve --port 4096 --hostname 0.0.0.0 &
OPENCODE_PID=$!

# Cleanup on exit
cleanup() {
    kill $OPENCODE_PID 2>/dev/null || true
}
trap cleanup EXIT

# Wait for server to be ready (readiness check)
for i in {1..60}; do
    if curl -s http://localhost:4096/global/health > /dev/null 2>&1; then
        break
    fi
    if [ $i -eq 60 ]; then
        send_callback "$(jq -n --arg id "$MINION_ID" --arg err "OpenCode server failed to start" \
            '{minion_id: $id, status: "failed", error: $err}')"
        exit 1
    fi
    sleep 1
done

# Create session and send task
SESSION_RESPONSE=$(curl -s -X POST http://localhost:4096/session \
    -H "Content-Type: application/json" \
    -d '{"title": "Minion task"}')
SESSION_ID=$(echo "$SESSION_RESPONSE" | jq -r '.id')

if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
    send_callback "$(jq -n --arg id "$MINION_ID" --arg err "Failed to create OpenCode session" \
        '{minion_id: $id, status: "failed", error: $err}')"
    exit 1
fi

# Notify orchestrator we're running (with session ID)
send_callback "$(jq -n --arg id "$MINION_ID" --arg sid "$SESSION_ID" \
    '{minion_id: $id, session_id: $sid, status: "running"}')"

# Safe JSON construction with jq (prevents shell injection)
TASK_PAYLOAD=$(jq -n --arg task "$TASK" '{parts: [{type: "text", text: $task}]}')

# Send the task message
RESULT=$(curl -s -X POST "http://localhost:4096/session/${SESSION_ID}/message" \
    -H "Content-Type: application/json" \
    -d "$TASK_PAYLOAD")

# Check for OpenCode errors
if echo "$RESULT" | jq -e '.error' > /dev/null 2>&1; then
    ERROR_MSG=$(echo "$RESULT" | jq -r '.error')
    send_callback "$(jq -n --arg id "$MINION_ID" --arg err "$ERROR_MSG" \
        '{minion_id: $id, status: "failed", error: $err}')"
    exit 1
fi

# Check if there are changes to commit
if git diff --quiet && git diff --staged --quiet && [[ -z "$(git status --porcelain)" ]]; then
    send_callback "$(jq -n --arg id "$MINION_ID" \
        '{minion_id: $id, status: "completed", message: "No changes needed"}')"
else
    # Create PR
    BRANCH="minion/${MINION_ID}"
    git checkout -b "$BRANCH"
    git add -A
    git commit -m "feat: ${TASK:0:50}"
    
    if ! git push origin "$BRANCH" 2>&1; then
        send_callback "$(jq -n --arg id "$MINION_ID" --arg err "Failed to push branch" \
            '{minion_id: $id, status: "failed", error: $err}')"
        exit 1
    fi
    
    PR_OUTPUT=$(gh pr create \
        --title "${TASK:0:80}" \
        --body "## Summary

This PR was automatically generated by Minion.

**Task:** ${TASK}

---
__Disclosure__
This change was developed with the assistance of AI, but should be reviewed by a human." \
        --head "$BRANCH" 2>&1) || {
        send_callback "$(jq -n --arg id "$MINION_ID" --arg err "$PR_OUTPUT" \
            '{minion_id: $id, status: "failed", error: $err}')"
        exit 1
    }
    
    PR_URL=$(echo "$PR_OUTPUT" | grep -E '^https://' | tail -1)
    send_callback "$(jq -n --arg id "$MINION_ID" --arg pr "$PR_URL" \
        '{minion_id: $id, status: "completed", pr_url: $pr}')"
fi
```

### Default OpenCode Config

```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5",
  "autoupdate": false,
  "share": "disabled",
  "permission": {
    "edit": "allow",
    "write": "allow",
    "bash": "allow",
    "read": "allow"
  }
}
```

---

## Phase 3: Orchestrator (Go)

**Deliverables:**
- Full Go service in `orchestrator/`
- K8s pod management
- SSE streaming from pods
- WebSocket hub for control panel
- Watchdog for idle detection

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/minions` | Create minion (body: repo, task, model, discord metadata) |
| `GET` | `/api/minions` | List minions (query: status, limit) |
| `GET` | `/api/minions/:id` | Get minion + recent events |
| `DELETE` | `/api/minions/:id` | Terminate: delete pod, update DB, notify Discord |
| `WS` | `/api/minions/:id/stream` | WebSocket: live events |
| `POST` | `/api/minions/:id/callback` | Pod reports completion/failure |
| `GET` | `/api/stats` | Aggregate costs (total, per-model breakdown) |
| `GET` | `/health` | Health check |

### Core Flows

1. **Create minion:**
   - Insert DB record (pending)
   - Build pod spec with env vars
   - Create K8s pod
   - Update DB (running, pod_name, started_at)
   - Spawn goroutine: connect to pod's OpenCode `/event` SSE
   - Forward events → DB + WebSocket hub

2. **Event streaming:**
   - SSE client connects to `http://<pod-ip>:4096/event`
   - Parse events, extract token usage, update `last_activity_at`
   - Accumulate `input_tokens`, `output_tokens`
   - Broadcast to WebSocket subscribers

3. **Watchdog (goroutine):**
   - Every 5 minutes: query minions WHERE status='running' AND last_activity_at < NOW() - 30min
   - For each stale minion: POST to Discord bot webhook with warning
   - Bot sends: "Minion for `repo` has been idle for 30+ minutes. Check status: <control panel link>"

4. **Termination:**
   - Delete K8s pod (force if needed)
   - Update DB: status=terminated, terminated_by, completed_at
   - Close SSE connection, notify WebSocket clients
   - Call Discord bot: "Task terminated by @user"

5. **Completion callback:**
   - Pod POSTs: `{minion_id, status, pr_url?, error?}`
   - Update DB: status, pr_url, completed_at
   - Call Discord bot: "PR created: <url>" or "Task failed: <error>"

### Environment Variables

```bash
# Database
DATABASE_URL=postgres://user:pass@host:5432/minions

# Kubernetes
KUBECONFIG=              # Empty for in-cluster
K8S_NAMESPACE=minions
DEVBOX_IMAGE=ghcr.io/imdevinc/minions/devbox:latest

# LLM providers (injected into pods)
ANTHROPIC_API_KEY=
OPENAI_API_KEY=
# ... other providers

# GitHub App (for pod auth)
GITHUB_APP_ID=
GITHUB_APP_PRIVATE_KEY=  # Base64 encoded
GITHUB_APP_INSTALLATION_ID=

# Discord (for notifications)
DISCORD_WEBHOOK_URL=     # Or direct bot API

# API Authentication (shared secret)
INTERNAL_API_TOKEN=      # Random 64-char string, shared with discord-bot and control-panel

# Server
PORT=8080
```

### Dependencies

- `github.com/go-chi/chi/v5` - routing
- `github.com/jackc/pgx/v5` - PostgreSQL
- `k8s.io/client-go` - Kubernetes
- `github.com/gorilla/websocket` - WebSocket
- `github.com/bradleyfalzon/ghinstallation/v2` - GitHub App token refresh
- `log/slog` - structured logging (stdlib)

### API Authentication Middleware

```go
// internal/api/middleware/auth.go
package middleware

import (
    "net/http"
    "os"
)

// RequireInternalAuth validates shared secret for internal service calls
func RequireInternalAuth(next http.Handler) http.Handler {
    token := os.Getenv("INTERNAL_API_TOKEN")
    if token == "" {
        panic("INTERNAL_API_TOKEN not set")
    }
    
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Allow health check without auth
        if r.URL.Path == "/health" {
            next.ServeHTTP(w, r)
            return
        }
        
        // Check Authorization header
        auth := r.Header.Get("Authorization")
        if auth != "Bearer "+token {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}

// RequireWebSocketAuth validates token in query param for WebSocket upgrade
func RequireWebSocketAuth(token string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // WebSocket auth via query param (since headers are limited)
            if r.URL.Query().Get("token") != token {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### GitHub App Token Management

```go
// internal/github/token.go
package github

import (
    "net/http"
    "github.com/bradleyfalzon/ghinstallation/v2"
)

type TokenProvider struct {
    transport *ghinstallation.Transport
}

func NewTokenProvider(appID int64, installationID int64, privateKey []byte) (*TokenProvider, error) {
    transport, err := ghinstallation.New(http.DefaultTransport, appID, installationID, privateKey)
    if err != nil {
        return nil, err
    }
    return &TokenProvider{transport: transport}, nil
}

// Token returns a valid installation token, automatically refreshing if expired
func (p *TokenProvider) Token() (string, error) {
    return p.transport.Token(context.Background())
}
```

### Orphaned Pod Cleanup (Startup Reconciliation)

```go
// internal/k8s/reconcile.go
func (c *Client) ReconcileOnStartup(ctx context.Context, db *db.Queries) error {
    // 1. List all pods with label app=devbox
    pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
        LabelSelector: "app=devbox",
    })
    if err != nil {
        return err
    }
    
    podMap := make(map[string]bool)
    for _, pod := range pods.Items {
        minionID := pod.Labels["minion-id"]
        podMap[minionID] = true
    }
    
    // 2. Get all minions with status=running from DB
    runningMinions, err := db.GetRunningMinions(ctx)
    if err != nil {
        return err
    }
    
    for _, minion := range runningMinions {
        if !podMap[minion.ID.String()] {
            // Pod doesn't exist but DB says running -> mark as failed
            db.UpdateMinionStatus(ctx, db.UpdateMinionStatusParams{
                ID:           minion.ID,
                Status:       "failed",
                ErrorMessage: sql.NullString{String: "Pod not found on startup (orphaned)", Valid: true},
            })
        }
    }
    
    // 3. Delete pods that don't have a corresponding running minion
    for _, pod := range pods.Items {
        minionID := pod.Labels["minion-id"]
        found := false
        for _, m := range runningMinions {
            if m.ID.String() == minionID {
                found = true
                break
            }
        }
        if !found {
            c.clientset.CoreV1().Pods(c.namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
        }
    }
    
    return nil
}
```

### Pod Creation with Retry

```go
// internal/k8s/pods.go
func (c *Client) CreateMinionPodWithRetry(ctx context.Context, spec *v1.Pod, maxRetries int) (*v1.Pod, error) {
    var pod *v1.Pod
    var err error
    
    backoff := time.Second
    for attempt := 1; attempt <= maxRetries; attempt++ {
        pod, err = c.clientset.CoreV1().Pods(c.namespace).Create(ctx, spec, metav1.CreateOptions{})
        if err == nil {
            return pod, nil
        }
        
        // Don't retry on certain errors
        if errors.IsInvalid(err) || errors.IsAlreadyExists(err) {
            return nil, err
        }
        
        slog.Warn("Pod creation failed", "attempt", attempt, "max", maxRetries, "error", err)
        if attempt < maxRetries {
            time.Sleep(backoff)
            backoff *= 2
        }
    }
    
    return nil, fmt.Errorf("pod creation failed after %d attempts: %w", maxRetries, err)
}
```

### SSE Client with Reconnection

```go
// internal/streaming/sse.go
func (s *SSEClient) StreamEventsWithReconnect(ctx context.Context, minionID uuid.UUID, podIP string) {
    backoff := time.Second
    maxBackoff := 30 * time.Second
    
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        
        err := s.streamEvents(ctx, minionID, podIP)
        if err == nil {
            return // Clean shutdown
        }
        
        // Check if pod still exists
        pod, _ := s.k8s.GetPod(ctx, fmt.Sprintf("minion-%s", minionID))
        if pod == nil || pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
            slog.Info("Pod terminated, stopping SSE stream", "minion_id", minionID)
            return
        }
        
        slog.Warn("SSE connection lost, reconnecting", "minion_id", minionID, "backoff", backoff)
        time.Sleep(backoff)
        backoff = min(backoff*2, maxBackoff)
    }
}
```

### Duplicate Minion Prevention

```go
// internal/db/minions.go
func (q *Queries) CreateMinionWithLock(ctx context.Context, params CreateMinionParams) (*Minion, error) {
    tx, err := q.db.Begin(ctx)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback(ctx)
    
    // Advisory lock on repo+task hash to prevent duplicates
    lockKey := int64(hash(params.Repo + params.Task))
    _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey)
    if err != nil {
        return nil, err
    }
    
    // Check for recent duplicate (same repo+task within 5 minutes)
    var existingID uuid.UUID
    err = tx.QueryRow(ctx, `
        SELECT id FROM minions 
        WHERE repo = $1 AND task = $2 
        AND created_at > NOW() - INTERVAL '5 minutes'
        AND status IN ('pending', 'running')
        LIMIT 1
    `, params.Repo, params.Task).Scan(&existingID)
    
    if err == nil {
        return nil, fmt.Errorf("duplicate minion already running: %s", existingID)
    }
    if err != pgx.ErrNoRows {
        return nil, err
    }
    
    // Create new minion
    minion, err := q.createMinionInternal(ctx, tx, params)
    if err != nil {
        return nil, err
    }
    
    return minion, tx.Commit(ctx)
}
```

### Graceful Shutdown

```go
// cmd/orchestrator/main.go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Initialize components...
    srv := &http.Server{Addr: ":8080", Handler: router}
    
    // Graceful shutdown on SIGTERM
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            slog.Error("Server error", "error", err)
            os.Exit(1)
        }
    }()
    
    <-quit
    slog.Info("Shutting down gracefully...")
    
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer shutdownCancel()
    
    // Stop accepting requests
    srv.Shutdown(shutdownCtx)
    
    // Cancel all SSE streams
    cancel()
    
    // Close DB pool
    dbPool.Close()
    
    slog.Info("Shutdown complete")
}
```

---

## Phase 4: Discord Bot (Go)

**Deliverables:**
- Discord bot in `discord-bot/`
- Command parsing
- One-round clarification flow
- Webhook handler for orchestrator callbacks

### Command Syntax

```
@minion --repo ImDevinC/go-fifa [--model anthropic/claude-opus-4-5] Add a /health endpoint
```

### Flow

1. **Parse command:**
   - Extract `--repo` (required)
   - Extract `--model` (optional, default from config)
   - Remaining text = task

2. **Clarification round:**
   - Use clarification model (default: same as task model, configurable)
   - Prompt: "You're about to implement this task for {repo}: {task}. If you need clarification, ask ONE brief question. If the task is clear, respond with exactly 'READY'."
   - If response != "READY": reply to Discord, wait for user response, append to task
   - Store clarification Q&A in DB for context

3. **Spawn minion:**
   - POST to orchestrator `/api/minions`
   - React with emoji (e.g., `:robot:`) to confirm
   - Reply: "Starting minion for `{repo}`... View progress: <control panel link>"

4. **Receive webhooks from orchestrator:**
   - Completion: reply with PR URL
   - Failure: reply with error summary
   - Termination: reply "Terminated by @{user}"
   - Idle warning: reply "Minion idle >30min, check: <link>"

### Clarification Prompt

```
You are about to implement a coding task. Review the task and repository.

Repository: %s
Task: %s

If the task is clear and you can proceed, respond with exactly: READY

If you need ONE clarifying question to implement this correctly, ask it briefly. Only ask if truly necessary.
```

### Async Clarification State Machine

Clarification must be non-blocking. Store state in DB, not in-memory.

```go
// internal/clarify/state.go
type ClarificationState string

const (
    StateNone           ClarificationState = "none"
    StatePending        ClarificationState = "pending"        // Waiting for LLM response
    StateAwaitingUser   ClarificationState = "awaiting_user"  // LLM asked question, waiting for user
    StateReady          ClarificationState = "ready"          // Ready to spawn minion
)

// DB columns on minions table:
// clarification_state TEXT DEFAULT 'none'
// clarification_question TEXT
// clarification_answer TEXT
// clarification_message_id TEXT  -- Discord message ID to track replies
```

```go
// internal/clarify/handler.go

// OnMinionCommand - called when user invokes @minion
func (h *Handler) OnMinionCommand(ctx context.Context, msg *discordgo.MessageCreate, repo, task, model string) {
    // Create minion in DB with clarification_state='pending'
    minion, err := h.db.CreateMinion(ctx, CreateMinionParams{
        Repo:               repo,
        Task:               task,
        Model:              model,
        DiscordMessageID:   msg.ID,
        DiscordChannelID:   msg.ChannelID,
        DiscordUserID:      msg.Author.ID,
        ClarificationState: StatePending,
    })
    
    // Kick off async LLM call (don't block)
    go h.runClarificationCheck(minion.ID, repo, task, model)
    
    // Acknowledge immediately
    h.session.MessageReactionAdd(msg.ChannelID, msg.ID, "🤔")
}

// runClarificationCheck - async goroutine
func (h *Handler) runClarificationCheck(minionID uuid.UUID, repo, task, model string) {
    prompt := fmt.Sprintf(clarificationPrompt, repo, task)
    response, err := h.llm.Complete(context.Background(), model, prompt)
    
    minion, _ := h.db.GetMinion(context.Background(), minionID)
    
    if strings.TrimSpace(response) == "READY" {
        // No clarification needed, spawn immediately
        h.db.UpdateClarificationState(context.Background(), minionID, StateReady)
        h.spawnMinion(minionID)
    } else {
        // LLM has a question, ask user
        reply, _ := h.session.ChannelMessageSendReply(
            minion.DiscordChannelID,
            fmt.Sprintf("**Clarification needed:**\n%s", response),
            &discordgo.MessageReference{MessageID: minion.DiscordMessageID},
        )
        h.db.UpdateClarificationQuestion(context.Background(), UpdateClarificationQuestionParams{
            ID:                     minionID,
            ClarificationState:     StateAwaitingUser,
            ClarificationQuestion:  response,
            ClarificationMessageID: reply.ID,
        })
    }
}

// OnMessageCreate - check if this is a reply to a clarification
func (h *Handler) OnMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.ReferencedMessage == nil {
        return
    }
    
    // Find minion awaiting clarification with this message ID
    minion, err := h.db.GetMinionByClarificationMessageID(ctx, m.ReferencedMessage.ID)
    if err != nil || minion.ClarificationState != StateAwaitingUser {
        return
    }
    
    // Only accept answer from original requester
    if m.Author.ID != minion.DiscordUserID {
        return
    }
    
    // Store answer and spawn
    h.db.UpdateClarificationAnswer(ctx, UpdateClarificationAnswerParams{
        ID:                   minion.ID,
        ClarificationState:   StateReady,
        ClarificationAnswer:  m.Content,
    })
    
    // Append clarification to task
    fullTask := fmt.Sprintf("%s\n\nClarification Q: %s\nA: %s",
        minion.Task, minion.ClarificationQuestion, m.Content)
    h.db.UpdateMinionTask(ctx, minion.ID, fullTask)
    
    h.session.MessageReactionAdd(m.ChannelID, m.ID, "👍")
    h.spawnMinion(minion.ID)
}
```

### Rate Limiting

```go
// internal/ratelimit/ratelimit.go
package ratelimit

import (
    "sync"
    "time"
)

type Limiter struct {
    store map[string]*userLimit
    mu    sync.RWMutex
}

type userLimit struct {
    count     int
    resetTime time.Time
}

func New() *Limiter {
    return &Limiter{store: make(map[string]*userLimit)}
}

// Allow checks if user can create another minion (max 10/hour, max 3 concurrent)
func (l *Limiter) Allow(userID string, maxPerHour int) bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    
    now := time.Now()
    limit, exists := l.store[userID]
    
    if !exists || now.After(limit.resetTime) {
        l.store[userID] = &userLimit{count: 1, resetTime: now.Add(time.Hour)}
        return true
    }
    
    if limit.count >= maxPerHour {
        return false
    }
    
    limit.count++
    return true
}

// Usage in command handler:
// if !h.rateLimiter.Allow(msg.Author.ID, 10) {
//     h.session.ChannelMessageSend(msg.ChannelID, "⏱️ Rate limit: max 10 minions per hour")
//     return
// }
// Also check concurrent limit in DB before creating minion
```

### Clarification Timeout Watchdog

```go
// internal/clarify/timeout.go
func (h *Handler) StartTimeoutWatchdog(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                h.cleanupTimedOutClarifications()
            }
        }
    }()
}

func (h *Handler) cleanupTimedOutClarifications() {
    ctx := context.Background()
    
    // Find clarifications older than 24 hours
    timedOut, err := h.db.GetTimedOutClarifications(ctx, 24*time.Hour)
    if err != nil {
        slog.Error("Failed to get timed out clarifications", "error", err)
        return
    }
    
    for _, minion := range timedOut {
        h.db.UpdateMinionStatus(ctx, UpdateMinionStatusParams{
            ID:           minion.ID,
            Status:       "failed",
            ErrorMessage: sql.NullString{String: "Clarification timeout: no response within 24 hours", Valid: true},
        })
        
        h.session.ChannelMessageSendReply(
            minion.DiscordChannelID,
            "❌ Minion cancelled: no clarification response within 24 hours. Run the command again if needed.",
            &discordgo.MessageReference{MessageID: minion.DiscordMessageID},
        )
        
        slog.Info("Timed out clarification", "minion_id", minion.ID)
    }
}

// SQL query for GetTimedOutClarifications:
// SELECT * FROM minions
// WHERE clarification_state = 'awaiting_user'
// AND created_at < NOW() - $1::INTERVAL;
```

### Dependencies

- `github.com/bwmarrin/discordgo`

---

## Phase 5: Control Panel (Next.js)

**Deliverables:**
- Next.js app in `control-panel/`
- Discord OAuth via NextAuth
- Dashboard with minion list
- Detail page with live event log
- Stats page with cost tracking

### Pages

| Route | Description |
|-------|-------------|
| `/` | Dashboard: minion cards with status, repo, created time |
| `/minions/:id` | Full detail: task, events log (live), terminate button, PR link |
| `/stats` | Cost table: per-minion costs, totals, model breakdown |

### Features

- **Live updates**: WebSocket to orchestrator, update React Query cache
- **Event log**: Virtual scroll for large logs, syntax highlighting for code blocks
- **Terminate**: Confirmation modal → DELETE `/api/minions/:id`
- **Status indicators**: pending (gray), running (blue pulse), completed (green), failed (red), terminated (orange)
- **Cost display**: Calculate from tokens using model pricing table

### Environment Variables

```bash
# NextAuth
NEXTAUTH_URL=https://minions.example.com
NEXTAUTH_SECRET=<random-secret>

# Discord OAuth
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=

# Orchestrator API
ORCHESTRATOR_URL=http://orchestrator:8080
INTERNAL_API_TOKEN=      # Same as orchestrator's INTERNAL_API_TOKEN
```

### Tech Stack

- shadcn/ui + Tailwind
- React Query (TanStack Query)
- WebSocket with reconnection logic
- next-auth for Discord OAuth

---

## Phase 6: Documentation

**Deliverables:**
- `docs/setup/github-app.md`
- `docs/setup/discord-app.md`
- `docs/setup/kubernetes.md`

---

## Setup Guides

### GitHub App Setup

#### Create the App

1. Go to https://github.com/settings/apps/new (or org settings for org-wide)

2. Fill in:
   - **App name**: `Minions Bot` (or your choice)
   - **Homepage URL**: Your control panel URL
   - **Webhook**: Disable (not needed)

3. **Permissions** (Repository):
   - Contents: Read & Write
   - Pull requests: Read & Write
   - Metadata: Read-only

4. **Where can this app be installed?**: 
   - "Only on this account" for personal
   - "Any account" if others will use

5. Click "Create GitHub App"

#### Generate Private Key

1. On the app page, scroll to "Private keys"
2. Click "Generate a private key"
3. Save the downloaded `.pem` file securely

#### Install the App

1. Go to "Install App" in sidebar
2. Select your account/org
3. Choose "All repositories" or select specific repos
4. Note the **Installation ID** from the URL after install:
   `https://github.com/settings/installations/INSTALLATION_ID`

#### Configure Environment

```bash
GITHUB_APP_ID=123456
GITHUB_APP_PRIVATE_KEY=$(base64 -w0 < your-app.pem)
GITHUB_APP_INSTALLATION_ID=12345678
```

---

### Discord Application Setup

#### Create Application

1. Go to https://discord.com/developers/applications
2. Click "New Application"
3. Name: `Minions` (or your choice)
4. Click "Create"

#### Configure Bot

1. Go to "Bot" in sidebar
2. Click "Add Bot" → "Yes, do it!"
3. Under "Privileged Gateway Intents", enable:
   - Message Content Intent (required to read commands)
4. Click "Reset Token" and save the token securely

#### Configure OAuth2

1. Go to "OAuth2" → "General"
2. Add redirect URL: `https://your-control-panel.com/api/auth/callback/discord`
3. Copy **Client ID** and **Client Secret**

#### Bot Permissions

1. Go to "OAuth2" → "URL Generator"
2. Select scopes:
   - `bot`
   - `applications.commands`
3. Select bot permissions:
   - Send Messages
   - Send Messages in Threads
   - Add Reactions
   - Read Message History
4. Copy the generated URL and use it to invite the bot to your server

#### Environment Variables

**For Discord Bot service:**
```bash
DISCORD_BOT_TOKEN=<bot-token-from-step-above>
```

**For Control Panel (NextAuth):**
```bash
DISCORD_CLIENT_ID=<oauth2-client-id>
DISCORD_CLIENT_SECRET=<oauth2-client-secret>
```

---

### Kubernetes Setup

#### Prerequisites

- Kubernetes cluster (1.25+)
- kubectl configured
- Container registry access (ghcr.io recommended)

#### Namespace

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: minions
  labels:
    # Enforce baseline pod security (prevents privilege escalation)
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

#### Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: minions-secrets
  namespace: minions
type: Opaque
stringData:
  DATABASE_URL: "postgres://..."
  ANTHROPIC_API_KEY: "sk-ant-..."
  OPENAI_API_KEY: "sk-..."
  GITHUB_APP_ID: "123456"
  GITHUB_APP_PRIVATE_KEY: "<base64-encoded-pem>"
  GITHUB_APP_INSTALLATION_ID: "12345678"
  DISCORD_BOT_TOKEN: "..."
  DISCORD_CLIENT_ID: "..."
  DISCORD_CLIENT_SECRET: "..."
  NEXTAUTH_SECRET: "<random-32-chars>"
  INTERNAL_API_TOKEN: "<random-64-chars>"  # Shared between orchestrator, discord-bot, control-panel
```

#### Orchestrator Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orchestrator
  namespace: minions
spec:
  replicas: 1
  selector:
    matchLabels:
      app: orchestrator
  template:
    metadata:
      labels:
        app: orchestrator
    spec:
      serviceAccountName: minions-orchestrator
      containers:
      - name: orchestrator
        image: ghcr.io/imdevinc/minions/orchestrator:latest
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
          failureThreshold: 3
        envFrom:
        - secretRef:
            name: minions-secrets
        env:
        - name: K8S_NAMESPACE
          value: minions
        - name: DEVBOX_IMAGE
          value: ghcr.io/imdevinc/minions/devbox:latest
---
apiVersion: v1
kind: Service
metadata:
  name: orchestrator
  namespace: minions
spec:
  selector:
    app: orchestrator
  ports:
  - port: 8080
```

#### RBAC for Pod Management

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: minions-orchestrator
  namespace: minions
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-manager
  namespace: minions
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create", "delete", "get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: orchestrator-pod-manager
  namespace: minions
subjects:
- kind: ServiceAccount
  name: minions-orchestrator
roleRef:
  kind: Role
  name: pod-manager
  apiGroup: rbac.authorization.k8s.io
```

#### Devbox Pod Template (created dynamically)

```yaml
# This is created by orchestrator, not applied manually
apiVersion: v1
kind: Pod
metadata:
  name: minion-<uuid>
  namespace: minions
  labels:
    app: devbox
    minion-id: <uuid>
spec:
  restartPolicy: Never
  # Pod-level security context
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: devbox
    image: ghcr.io/imdevinc/minions/devbox:latest
    ports:
    - containerPort: 4096
    # Container-level security context
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: false  # Required for git, npm, etc.
      capabilities:
        drop: ["ALL"]
    readinessProbe:
      httpGet:
        path: /global/health
        port: 4096
      initialDelaySeconds: 5
      periodSeconds: 2
      failureThreshold: 30
    env:
    - name: REPO
      value: "ImDevinC/go-fifa"
    - name: TASK
      value: "Add health check endpoint"
    - name: MINION_ID
      value: "<uuid>"
    - name: CALLBACK_URL
      value: "http://orchestrator:8080/api/minions/<uuid>/callback"
    - name: GITHUB_TOKEN
      value: "<installation-token>"
    - name: ANTHROPIC_API_KEY
      valueFrom:
        secretKeyRef:
          name: minions-secrets
          key: ANTHROPIC_API_KEY
    resources:
      requests:
        cpu: "1"
        memory: "2Gi"
      limits:
        cpu: "2"
        memory: "4Gi"
```

#### Network Policy (Required for pod-to-orchestrator callbacks)

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: devbox-egress
  namespace: minions
spec:
  podSelector:
    matchLabels:
      app: devbox
  policyTypes:
  - Egress
  egress:
  # Allow DNS resolution (required for github.com, api.anthropic.com, etc.)
  - to: []
    ports:
    - port: 53
      protocol: UDP
    - port: 53
      protocol: TCP
  # Allow HTTPS to GitHub API and LLM providers
  - to: []
    ports:
    - port: 443
      protocol: TCP
  # Allow callback to orchestrator (REQUIRED)
  - to:
    - podSelector:
        matchLabels:
          app: orchestrator
    ports:
    - port: 8080
      protocol: TCP
---
# Allow orchestrator to receive callbacks and connect to devbox SSE
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: orchestrator-ingress
  namespace: minions
spec:
  podSelector:
    matchLabels:
      app: orchestrator
  policyTypes:
  - Ingress
  ingress:
  # From devbox pods (callbacks)
  - from:
    - podSelector:
        matchLabels:
          app: devbox
    ports:
    - port: 8080
  # From control panel
  - from:
    - podSelector:
        matchLabels:
          app: control-panel
    ports:
    - port: 8080
  # From external ingress (Discord webhooks, etc.)
  - from: []
    ports:
    - port: 8080
```

#### Resource Recommendations

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-----------|-------------|-----------|----------------|--------------|
| Orchestrator | 100m | 500m | 128Mi | 512Mi |
| Discord Bot | 50m | 200m | 64Mi | 256Mi |
| Control Panel | 100m | 500m | 128Mi | 512Mi |
| Devbox Pod | 1000m | 2000m | 2Gi | 4Gi |

---

## Implementation Order

| Phase | Component | Estimated Effort |
|-------|-----------|------------------|
| 1 | Foundation (README, schema, docs structure) | 1 day |
| 2 | Devbox Image | 1-2 days |
| 3 | Orchestrator | 3-4 days |
| 4 | Discord Bot | 2 days |
| 5 | Control Panel | 3-4 days |
| 6 | Documentation | 1 day |
| 7 | Integration Testing | 1-2 days |

**Total: ~12-16 days**

---

## Design Decisions

### Why OpenCode?

- Full HTTP API with SSE event streaming
- `opencode serve` for headless operation
- Built-in session management
- Token usage tracking
- Configurable models via JSON config or env vars

### Why Log Streaming?

OpenCode's `GET /event` endpoint provides SSE (Server-Sent Events). The flow:

1. Pod runs `opencode serve` and starts a session
2. Orchestrator connects to pod's `/event` endpoint via SSE
3. Orchestrator rebroadcasts events to:
   - PostgreSQL (for persistence/history)
   - WebSocket clients (control panel viewers)
4. When pod terminates, orchestrator has full log history

This enables:
- **Live viewing** in control panel
- **Historical viewing** after minion completes
- **Cost tracking** (OpenCode emits token usage events)

### Why GitHub App vs PAT?

- Scoped permissions (only what's needed)
- Per-installation tokens (better audit trail)
- No user token expiration issues
- Works across org repositories

### Why No Hard Timeout?

- Some tasks legitimately take a long time
- Watchdog alerts after 30min idle, human decides
- User can always terminate from control panel

### Why New Minion for Follow-ups?

- Stateless design (simpler)
- Clean git state for each task
- No persistent volume complexity
- Matches Stripe's "one-shot" philosophy

---

## Model Defaults

```json
{
  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5"
}
```

Configurable per-task via `--model` flag in Discord command.

Clarification model defaults to task model but can be overridden.
