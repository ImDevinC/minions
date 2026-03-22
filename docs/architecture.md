# Architecture

## System Overview

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
│         │            │  • Watchdog (30min idle alerts)                     │   │
│         │            └─────────────────────────────────────────────────────┘   │
│         │                              │                                        │
│         │              ┌───────────────┼───────────────┐                        │
│         │              ▼               ▼               ▼                        │
│         │         ┌─────────┐    ┌──────────┐    ┌──────────┐                   │
│         │         │ Pod #1  │    │ Pod #2   │    │ Pod #N   │                   │
│         │         │         │    │          │    │          │                   │
│         │         │opencode │    │opencode  │    │opencode  │                   │
│         │         │ serve   │    │ serve    │    │ serve    │                   │
│         │         │ :4096   │    │ :4096    │    │ :4096    │                   │
│         │         └────┬────┘    └──────────┘    └──────────┘                   │
│         │              │                                                        │
│         │              │ SSE events                                             │
│         │              ▼                                                        │
│         └──────────────┴──── PR URL / termination / errors ────────────────────│
│                                                                                 │
│   ┌────────────────────────────────────────────────────────────────────────┐   │
│   │                    Control Panel (Next.js + React)                      │   │
│   │  • Discord OAuth login                                                  │   │
│   │  • List all minions (any authed user sees all)                          │   │
│   │  • Live event log (WebSocket)                                           │   │
│   │  • Terminate button                                                     │   │
│   │  • Cost tracking display                                                │   │
│   └────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
│                         ┌─────────────────────┐                                 │
│                         │     PostgreSQL      │                                 │
│                         │  • users            │                                 │
│                         │  • minions          │                                 │
│                         │  • minion_events    │                                 │
│                         └─────────────────────┘                                 │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Data Flow

### 1. Command Invocation

```
User: @minion --repo ImDevinC/go-fifa Add a /health endpoint
                    │
                    ▼
         ┌──────────────────┐
         │   Discord Bot    │
         │                  │
         │ 1. Parse command │
         │ 2. Insert minion │
         │    (state=pending│
         │    clarify=pend) │
         └────────┬─────────┘
                  │
                  ▼ async
         ┌──────────────────┐
         │ Clarification    │
         │ LLM Call         │
         │                  │
         │ "Is X or Y?"     │
         │  or "READY"      │
         └────────┬─────────┘
                  │
        ┌─────────┴─────────┐
        ▼                   ▼
   [Question]          [READY]
        │                   │
        ▼                   ▼
   Reply to Discord    Spawn Minion
   Wait for answer          │
        │                   │
        ▼                   │
   User replies ────────────┘
```

### 2. Pod Lifecycle

```
┌──────────────┐
│ Orchestrator │
│              │
│ POST /minion │
└──────┬───────┘
       │
       │ 1. Generate GitHub App token
       │ 2. Create K8s pod
       │ 3. Wait for readiness probe
       │
       ▼
┌──────────────────────────────────────┐
│           Devbox Pod                 │
│                                      │
│  entrypoint.sh:                      │
│  1. gh auth login                    │
│  2. git clone                        │
│  3. opencode serve :4096             │
│  4. POST /session                    │
│  5. POST /session/:id/message        │
│  6. Wait for completion              │
│  7. git commit && gh pr create       │
│  8. POST callback to orchestrator    │
│                                      │
└──────────────────────────────────────┘
       │
       │ Orchestrator connects to pod:4096/event (SSE)
       │ Receives: tool calls, assistant messages, token usage
       │
       ▼
┌──────────────┐
│  PostgreSQL  │
│              │
│ minion_events│ (stored for history)
│              │
└──────────────┘
       │
       │ WebSocket broadcast
       ▼
┌──────────────┐
│Control Panel │
│              │
│ Live updates │
└──────────────┘
```

### 3. Event Streaming

```
Pod (SSE)                  Orchestrator              Control Panel
    │                           │                         │
    │──event: message_part─────▶│                         │
    │                           │──INSERT minion_events──▶│
    │                           │──WebSocket broadcast───▶│
    │                           │                         │
    │──event: tool_call────────▶│                         │
    │                           │──INSERT minion_events──▶│
    │                           │──WebSocket broadcast───▶│
    │                           │                         │
    │──event: usage────────────▶│                         │
    │                           │──UPDATE tokens/cost────▶│
    │                           │──WebSocket broadcast───▶│
    │                           │                         │
```

## Key Design Decisions

### Stateless Minions

Each task gets a fresh pod with a clean git clone. No state carries over between tasks. Follow-up work spawns a new minion.

**Why?**
- Simpler (no persistent volumes)
- Cleaner git state
- Matches "one-shot" philosophy
- Easier to reason about failures

### Async Clarification

The clarification flow uses a state machine stored in PostgreSQL, not blocking goroutines.

```
States:
  none ──▶ pending ──▶ awaiting_user ──▶ ready ──▶ (spawn)
                  │                             ▲
                  └─────── READY ───────────────┘
```

**Why?**
- Discord bot can restart without losing state
- No goroutine leaks
- User can reply hours later
- Easier to debug (state visible in DB)

### GitHub App Tokens

Tokens expire after 1 hour. We use `ghinstallation` library which handles refresh automatically.

```go
transport, _ := ghinstallation.New(http.DefaultTransport, appID, installationID, privateKey)
// transport.Token() always returns a valid token
```

### No Hard Timeout

Minions run until completion, failure, or manual termination. A watchdog alerts Discord after 30 minutes of inactivity, but doesn't kill the pod.

**Why?**
- Some legitimate tasks take hours
- Human decides when to kill
- Avoid wasted work from premature termination

## Database Schema

### Tables

**users**: Discord OAuth sessions
```sql
id, discord_id, discord_username, avatar_url, created_at
```

**minions**: Task runs
```sql
id, status, repo, task,
clarification_state, clarification_question, clarification_answer, clarification_message_id,
model, discord_*, pod_*, session_id,
pr_url, error_message, terminated_by_discord_id,
input_tokens, output_tokens, estimated_cost_usd,
last_activity_at, created_at, started_at, completed_at
```

**minion_events**: OpenCode event log
```sql
id, minion_id, timestamp, event_type, content (JSONB)
```

### Indexes

- `idx_minions_status`: Filter by status
- `idx_minions_running_activity`: Watchdog queries (partial index)
- `idx_minions_clarification_msg`: Lookup by Discord message ID (partial index)
- `idx_minion_events_minion`: Event history by minion

## Network Topology

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Namespace: minions            │
│                                                             │
│   ┌─────────────┐                                           │
│   │ Orchestrator│◀──────────────────────────────────────┐   │
│   │   :8080     │                                       │   │
│   └──────┬──────┘                                       │   │
│          │                                              │   │
│   ┌──────┴──────┐      ┌─────────────────────────────┐  │   │
│   │             │      │                             │  │   │
│   ▼             ▼      ▼                             │  │   │
│ ┌──────┐    ┌──────┐ ┌──────┐                        │  │   │
│ │Pod A │    │Pod B │ │Pod C │  (devbox pods)         │  │   │
│ │:4096 │    │:4096 │ │:4096 │                        │  │   │
│ └──┬───┘    └──────┘ └──────┘                        │  │   │
│    │                                                 │  │   │
│    └── callback POST ────────────────────────────────┘  │   │
│                                                         │   │
│   ┌─────────────┐     ┌─────────────┐                   │   │
│   │ Discord Bot │     │Control Panel│                   │   │
│   │             │────▶│   :3000     │                   │   │
│   └─────────────┘     └─────────────┘                   │   │
│                                                         │   │
└─────────────────────────────────────────────────────────────┘

External:
  - Discord API (bot commands, webhooks)
  - GitHub API (clone, push, PR)
  - LLM APIs (Anthropic, OpenAI)
```

## Security Considerations

1. **API Authentication**: Orchestrator API protected by shared secret (`INTERNAL_API_TOKEN`). Discord bot and control panel must include `Authorization: Bearer <token>` header.
2. **Pod Security Context**: Devbox pods run with restricted privileges:
   - `runAsNonRoot: true`
   - `allowPrivilegeEscalation: false`
   - `capabilities: drop: ["ALL"]`
   - `seccompProfile: RuntimeDefault`
3. **Pod isolation**: Each minion runs in its own pod, cannot access other pods
4. **Scoped GitHub tokens**: GitHub App with minimal permissions (contents, PRs). Token passed via env var (never logged/echoed).
5. **Network policies**: Devbox pods can only reach DNS (53), HTTPS (443), and orchestrator (8080)
6. **Rate limiting**: Max 10 minions/hour per user, max 3 concurrent per user
7. **No secrets in logs**: Token values never logged, only usage counts
8. **Auth on control panel**: Discord OAuth required, any authed user can view/terminate (MVP)

## Failure Modes

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Pod crash | K8s event, callback timeout | Mark failed, notify Discord |
| Pod creation failure | K8s API error | Retry with exponential backoff (max 5 attempts) |
| Orchestrator restart | N/A | Reconcile DB with K8s pods on startup |
| SSE disconnect | Connection error | Reconnect with backoff (max 30s) |
| GitHub token expired | API 401 | `ghinstallation` auto-refresh |
| LLM API error | OpenCode error event | Stored in events, visible in UI |
| Idle minion | Watchdog query (5min interval) | Alert Discord after 30min, human decides |
| Duplicate minion | Advisory lock in DB | Reject with link to existing minion |
| Clarification timeout | 24h TTL watchdog | Auto-fail, notify Discord |
| Callback failure | HTTP error from pod | Retry with exponential backoff (max 5 attempts) |

## Cost Calculation

Token usage comes from OpenCode's `usage` events:

```json
{"type": "usage", "input_tokens": 1234, "output_tokens": 567}
```

Pricing table (USD per 1M tokens):

| Model | Input | Output |
|-------|-------|--------|
| claude-opus-4-5 | $15.00 | $75.00 |
| claude-sonnet-4-5 | $3.00 | $15.00 |
| claude-haiku-4-5 | $0.25 | $1.25 |
| gpt-4o | $2.50 | $10.00 |
| gpt-4o-mini | $0.15 | $0.60 |

Formula:
```
cost = (input_tokens / 1M * input_price) + (output_tokens / 1M * output_price)
```
