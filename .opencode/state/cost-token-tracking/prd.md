# PRD: Cost and Token Tracking

**Date:** 2026-03-24  

---

## Problem Statement

### What problem are we solving?

The minions orchestrator currently displays **$0.00 cost and 0 tokens** on all minion detail pages and the stats page, despite minions successfully executing tasks that consume AI API tokens. This breaks cost visibility for developers using the platform.

**User impact:**
- Developers cannot see the cost of their AI-powered tasks
- No way to identify expensive tasks or optimize token usage
- Platform feels incomplete and untrustworthy without basic metrics

**Technical root cause:**
The orchestrator's `extractTokenUsage()` function in `orchestrator/internal/streaming/sse.go` searches for the wrong event structure. It looks for top-level `token_usage` or `usage` fields, but OpenCode actually sends cost and token data nested inside `message.updated` events at `content.content.info.tokens` and `content.content.info.cost`.

### Why now?

This was discovered during internal development testing. Without cost tracking, developers have zero visibility into API spend, making the platform unsuitable for production use where cost monitoring is critical.

### Who is affected?

- **Primary users:** Individual developers running minions who need to understand the cost of their tasks
- **Secondary users:** Future team leads/finance teams who will need aggregated cost reporting (once platform scales)

---

## Proposed Solution

### Overview

Fix the cost and token tracking bug by updating the SSE event parser to extract data from the correct location in OpenCode's `message.updated` events. Store detailed token breakdowns (input, output, reasoning, cache read/write) in the database to enable future cost optimization features, while displaying simplified combined totals to users (input tokens = input + cache reads; output tokens = output + reasoning + cache writes).

### User Experience

#### User Flow: Developer Checks Task Cost
1. Developer submits a minion task via Discord
2. Minion executes the task (OpenCode sends SSE events with cost/token data)
3. Developer navigates to control panel `/minions/[id]` page
4. Page displays:
   - **Real-time updating** cost in USD (e.g., "$0.02954")
   - **Real-time updating** input tokens (e.g., "15,203 tokens")
   - **Real-time updating** output tokens (e.g., "1,722 tokens")
5. Metrics update live as the task progresses
6. Final accurate totals shown when task completes

#### User Flow: Platform Admin Reviews Stats
1. Admin navigates to control panel `/stats` page
2. Page displays:
   - **Total cost** across all minions (e.g., "$127.43")
   - **Total tokens** (input/output) across all minions
   - **Cost by model** breakdown (e.g., "claude-sonnet-4.5: $89.21")
3. All metrics reflect actual usage from database

---

## End State

When this PRD is complete, the following will be true:

- [ ] Minion detail pages display accurate real-time cost and token counts (combined input/output)
- [ ] Stats page displays accurate aggregate cost and token metrics across all minions
- [ ] Database stores detailed token breakdowns (input, output, reasoning, cache_read, cache_write) for future optimization features
- [ ] SSE event parser correctly extracts cost/token data from OpenCode `message.updated` events
- [ ] Token tracking is atomic (no race conditions or lost updates during concurrent events)
- [ ] Existing minions with $0 cost remain at $0 (no backfill - they weren't tracked)
- [ ] All database migrations run successfully
- [ ] Orchestrator and control panel rebuild and deploy without errors

---

## Success Metrics

### Quantitative
| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Minions showing $0.00 cost | 100% | 0% | Query minions where cost_usd = 0 AND status = completed |
| Minions showing 0 tokens | 100% | 0% | Query minions where input_tokens = 0 AND status = completed |
| Stats page total cost | $0.00 | Matches sum of all minions | Compare frontend display to `SELECT SUM(cost_usd) FROM minions` |

### Qualitative
- Developer confidence in platform increases (cost data matches expectations based on task complexity)
- Platform feels production-ready with proper observability

---

## Acceptance Criteria

### Database Schema
- [ ] Migration `007_add_token_detail_fields.sql` adds 3 new BIGINT columns: `reasoning_tokens`, `cache_read_tokens`, `cache_write_tokens`
- [ ] Migration runs successfully against existing database
- [ ] All new fields default to 0 (NOT NULL)

### Go Backend - Data Layer (`orchestrator/internal/db/minions.go`)
- [ ] `Minion` struct includes `ReasoningTokens`, `CacheReadTokens`, `CacheWriteTokens` fields
- [ ] `UpdateTokenUsageParams` struct includes 4 new fields: `ReasoningTokens`, `CacheReadTokens`, `CacheWriteTokens`, `CostUSD`
- [ ] `UpdateTokenUsage()` SQL uses atomic increments for all 6 fields: `SET input_tokens = input_tokens + $1, output_tokens = output_tokens + $2, reasoning_tokens = reasoning_tokens + $3, cache_read_tokens = cache_read_tokens + $4, cache_write_tokens = cache_write_tokens + $5, cost_usd = cost_usd + $6`
- [ ] All 9 SELECT queries include the 3 new token fields in column lists
- [ ] `scanMinion()` and `scanMinionWithOwner()` functions scan all new fields
- [ ] `GetStats()` combines token fields in SQL queries: `SUM(input_tokens + cache_read_tokens)` for input, `SUM(output_tokens + reasoning_tokens + cache_write_tokens)` for output

### Go Backend - Event Processing (`orchestrator/internal/streaming/`)
- [ ] `TokenUsage` struct in `sse.go` includes `ReasoningTokens`, `CacheReadTokens`, `CacheWriteTokens`, `CostUSD` fields
- [ ] `extractTokenUsage()` in `sse.go` correctly parses `message.updated` events with nested structure: `event.Content["content"].(map[string]any)["info"]`
- [ ] `extractTokenUsage()` extracts cost from `info.cost` using safe type assertion: `if cost, ok := info["cost"].(float64); ok`
- [ ] `extractTokenUsage()` extracts tokens from `info.tokens.input`, `.output`, `.reasoning`, `.cache.read`, `.cache.write`
- [ ] `extractTokenUsage()` handles missing or malformed fields gracefully (returns zero values, does not crash)
- [ ] `extractTokenUsage()` logs warning when no data extracted (cost=0, all tokens=0) to detect OpenCode format changes
- [ ] `extractTokenUsage()` logs debug-level message on successful extraction with cost and token values for observability
- [ ] `DBEventHandler.HandleTokenUsage()` in `handler.go` passes all extracted fields to `UpdateTokenUsage()`
- [ ] `NoOpEventHandler.HandleTokenUsage()` logs all new fields for debugging

### Frontend (`control-panel/`)
- [ ] `MinionDetail` TypeScript interface in `types/minion.ts` includes optional fields: `reasoning_tokens?`, `cache_read_tokens?`, `cache_write_tokens?`
- [ ] Minion detail client component (`components/minion-detail-client.tsx`) handles missing optional fields with null coalescing:
  - Input tokens = `(minion.input_tokens || 0) + (minion.cache_read_tokens || 0)`
  - Output tokens = `(minion.output_tokens || 0) + (minion.reasoning_tokens || 0) + (minion.cache_write_tokens || 0)`
- [ ] Cost displays with 5 decimal places (e.g., "$0.02954")
- [ ] Token counts display with thousands separators (e.g., "15,203")
- [ ] Minion detail component polls API every 3 seconds when minion status is `running` or `pending` for near-realtime cost updates
- [ ] Polling stops automatically when minion reaches terminal status (`completed`, `failed`, `cancelled`)

### Testing & Deployment
- [ ] Orchestrator rebuilds without errors (`cd orchestrator && go build ./...`)
- [ ] Control panel rebuilds without errors (`cd control-panel && npm run build`)
- [ ] Integration test `TestExtractTokenUsage_MessageUpdated` validates extraction using fake SSE event data from `testdata/message_updated.json`
- [ ] Fake SSE test data matches OpenCode event format: `message.updated` with nested `content.content.info.tokens` structure
- [ ] Integration test validates safe float64 conversion for cost field and all token fields
- [ ] Manual test: Create a new minion and verify cost/tokens appear on detail page
- [ ] Manual test: Verify stats page shows non-zero totals after test minion completes
- [ ] Manual test: Verify cost/tokens update while minion is running (observe page every 3 seconds without manual refresh)
- [ ] Manual test: Check orchestrator logs for debug-level token extraction success messages
- [ ] Manual test: Verify warning log appears when extraction fails (can simulate with malformed test event)

---

## Technical Context

### Existing Patterns

**Atomic token updates:**
- Pattern: `orchestrator/internal/db/minions.go:UpdateTokenUsage()` - Uses PostgreSQL atomic increments to avoid race conditions
- Relevance: New token fields must follow same pattern (increment, not overwrite)

**SSE event extraction:**
- Pattern: `orchestrator/internal/streaming/sse.go:extractTokenUsage()` - Type-safe extraction with nil checks
- Relevance: Must handle nested `content.content.info` structure safely

**Database scanning:**
- Pattern: `orchestrator/internal/db/minions.go:scanMinion()` - Centralized row scanning for consistency
- Relevance: All 9 queries use this helper; must update it to include new fields

**Stats aggregation:**
- Pattern: `orchestrator/internal/db/minions.go:GetStats()` - Server-side SUM() queries
- Relevance: Backend must combine token types before sending to frontend

**Near-realtime updates:**
- Pattern: Control panel polls API every 3 seconds for running minions
- Relevance: Provides near-realtime cost visibility without SSE complexity (SSE doesn't include historical data)

**Token extraction observability:**
- Pattern: Debug logging on success, warning logging on failure (all zeros extracted)
- Relevance: Enables monitoring of extraction health and debugging OpenCode format changes

### Key Files

- `schema/migrations/007_add_token_detail_fields.sql` - **CREATE NEW** database migration
- `orchestrator/internal/db/minions.go` - Data layer (struct definitions, queries, scanning)
- `orchestrator/internal/streaming/sse.go` - SSE event parsing and token extraction
- `orchestrator/internal/streaming/handler.go` - Event handler that calls database updates
- `control-panel/src/types/minion.ts` - TypeScript type definitions
- `control-panel/src/components/minion-detail-client.tsx` - UI component displaying tokens/cost

### System Dependencies

**External services:**
- OpenCode pods serve SSE events at `:4096/events` (HTTP, no auth - trusted pod network)
- Events sent in real-time as task executes

**Data model:**
- PostgreSQL `minions` table stores all token/cost data
- Numeric(12,6) precision for `cost_usd` (supports $999,999.999999)
- BIGINT for token counts (supports up to 9 quintillion tokens)

### Data Model Changes

**New fields in `minions` table:**
```sql
reasoning_tokens BIGINT NOT NULL DEFAULT 0
cache_read_tokens BIGINT NOT NULL DEFAULT 0  
cache_write_tokens BIGINT NOT NULL DEFAULT 0
```

**Migration approach:**
- Add columns with DEFAULT 0 (fast operation, no table rewrite)
- No backfill needed (existing minions remain at 0 - they weren't tracked)
- No rollback migration (columns can safely remain even if code reverted)

**OpenCode event structure:**
```json
{
  "type": "message.updated",
  "content": {
    "content": {
      "info": {
        "cost": 0.02954525,
        "tokens": {
          "input": 1763,
          "output": 929,
          "reasoning": 793,
          "total": 16132,
          "cache": {"read": 13440, "write": 0}
        }
      }
    }
  }
}
```

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Race condition: concurrent SSE events update same minion | High | Med | Use atomic SQL increments (already implemented in `UpdateTokenUsage`) |
| OpenCode changes event format | Low | High | Add integration test with sample SSE data; version-pin OpenCode container if needed |
| Migration fails on production | Low | High | Test migration locally first; migration is non-destructive (ADD COLUMN only) |
| Frontend type mismatch (old API, new types) | Med | Low | Make new fields optional (`?`) in TypeScript; backend always sends them but old data is 0 |
| Token counts exceed BIGINT max | Very Low | Low | BIGINT supports 9 quintillion tokens; would require $billions in API costs to hit limit |

---

## Alternatives Considered

### Alternative 1: Backfill existing minions with estimated costs
- **Description:** Query OpenCode API to recalculate costs for old minions
- **Pros:** Historical data would be complete
- **Cons:** OpenCode doesn't store historical token data; estimation would be inaccurate and misleading
- **Decision:** Rejected. Better to show accurate $0 for old minions than false estimates.

### Alternative 2: Store only combined token totals (no cache/reasoning breakdown)
- **Description:** Keep database simple with just `input_tokens` and `output_tokens`
- **Pros:** Fewer database columns and simpler code
- **Cons:** Lose granular data needed for future cost optimization features (e.g., "you're paying for cache writes, enable prompt caching")
- **Decision:** Rejected. Storage is cheap; detailed breakdown enables future value.

### Alternative 3: Parse token data client-side from SSE stream
- **Description:** Send raw SSE events to browser and extract tokens in JavaScript
- **Pros:** Simpler backend
- **Cons:** No persistent storage; data lost on page refresh; can't aggregate stats across minions
- **Decision:** Rejected. Database persistence is essential for stats and historical analysis.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Cost budgets/limits** - Auto-stop minions when costs exceed threshold → Deferred to future feature
- **Multi-currency support** - Display costs in EUR/GBP/etc → Deferred (USD sufficient for v1)
- **Cost optimization suggestions** - "Use prompt caching to reduce cost by X%" → Deferred (need data baseline first)
- **Cost attribution/tagging** - Tag minions by project/team for chargeback → Deferred (single-user platform currently)
- **Export to billing systems** - Push costs to external accounting tools → Deferred (no requirement yet)
- **Historical cost trends** - Charts showing cost over time → Deferred (stats page shows totals only)
- **Per-event cost tracking** - Store cost for each individual SSE event → Deferred (minion-level totals sufficient)

---

## Documentation Requirements

- [ ] Update `AGENTS.md` to document token tracking implementation (for future AI agents working on codebase)
- [ ] Add code comments in `extractTokenUsage()` explaining OpenCode event structure
- [ ] No user-facing docs needed (UI is self-explanatory)

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| Should we display cache read/write tokens separately in UI (advanced mode)? | Product | Before implementation | Open |
| Do we need alerts when a single minion exceeds $X cost? | Product | Before v2 planning | Open |

---

## Appendix

### Glossary

- **Minion:** A single task execution instance (ephemeral Kubernetes pod running OpenCode)
- **SSE (Server-Sent Events):** HTTP streaming protocol used by OpenCode pods to send real-time events
- **OpenCode:** The AI coding agent running inside devbox pods
- **Token:** Unit of text processed by AI models (roughly 4 characters per token)
- **Input tokens:** Tokens sent to the AI model (prompt, context, code)
- **Output tokens:** Tokens generated by the AI model (responses, code edits)
- **Reasoning tokens:** Extended thinking tokens used by models like Claude (counted as output for billing)
- **Cache tokens:** Tokens stored/retrieved from model context caching (read = input, write = output)

### References

- Sample SSE event data: `/home/devin/Projects/minions/sse` (captured from live OpenCode pod)
- Database schema: `schema/migrations/002_create_minions_table.sql`
- OpenCode SSE endpoint: Served at `:4096/events` on each devbox pod
- Token extraction implementation: `orchestrator/internal/streaming/sse.go:extractTokenUsage()`
