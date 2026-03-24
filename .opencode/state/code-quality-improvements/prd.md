# PRD: Code Quality & Security Improvements

**Date:** 2026-03-24

---

## Problem Statement

### What problem are we solving?

A comprehensive code review of the Minions codebase identified several issues across security, reliability, and maintainability:

1. **Security gaps**: API handlers accept unbounded request bodies (potential DoS), task content has no length validation at API level, and any Discord user can answer another user's clarification questions.

2. **Reliability issues**: Discord bot handlers use `context.Background()` preventing graceful shutdown, and OpenCode processes aren't killed on task timeout.

3. **Code quality debt**: Custom error matching via string comparison instead of `errors.Is()`, duplicate scan patterns repeated 8+ times, unused code (`RateLimitError`, custom `itoa`), and thinking emoji never removed after processing.

### Why now?

Proactive tech debt reduction. The codebase is functional but these issues increase maintenance burden and pose latent risks:
- Unbounded request bodies are a vector for resource exhaustion attacks
- Non-cancellable contexts prevent clean shutdown during orchestrator restarts
- Code duplication increases risk of inconsistent bug fixes

### Who is affected?

- **Primary users:** Discord users creating minion tasks (clarification security)
- **Secondary users:** Operators running the orchestrator (reliability, observability)
- **Developers:** Engineers maintaining the codebase (code quality)

---

## Proposed Solution

### Overview

Address all identified issues across five categories: security hardening, context/error handling, devbox cleanup, and general code quality. Each fix is self-contained and can be deployed incrementally.

---

## End State

When this PRD is complete, the following will be true:

- [ ] All API handlers enforce 1MB request body limit
- [ ] Task content validated to 10,000 character max at API level
- [ ] Clarification replies validated to ensure only original requester can answer
- [ ] Discord bot handlers use timeout contexts (30s) instead of `context.Background()`
- [ ] Error matching uses `errors.Is()` / `errors.As()` instead of string comparison
- [ ] OpenCode process killed on task timeout in devbox
- [ ] Dead code removed (`RateLimitError`, custom `itoa`)
- [ ] Duplicate scan patterns extracted to helper function
- [ ] Thinking emoji removed after processing completes
- [ ] All existing tests pass
- [ ] New tests added for security validation logic
- [ ] Go services build successfully

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| API handlers with body limits | 0/3 | 3/3 | Code audit |
| `context.Background()` in handlers | 4 | 0 | grep count |
| Duplicate scan patterns | 8+ | 1 (shared helper) | Code audit |
| Custom string-based error matching | 3 functions | 0 | Code audit |

### Qualitative

- Reduced maintenance burden from code deduplication
- Cleaner graceful shutdown behavior
- Consistent error handling patterns

---

## Acceptance Criteria

### Security: Request Body Limits

- [ ] `HandleCreate` wraps `r.Body` with `http.MaxBytesReader(w, r.Body, 1<<20)`
- [ ] `HandleCallback` wraps `r.Body` with `http.MaxBytesReader`
- [ ] `HandleClarificationAnswer` wraps `r.Body` with `http.MaxBytesReader`
- [ ] Requests exceeding 1MB return 400 Bad Request

### Security: Task Length Validation

- [ ] Constant `MaxTaskLength = 10000` defined in orchestrator API package
- [ ] `HandleCreate` rejects tasks exceeding `MaxTaskLength` with 400 and code `TASK_TOO_LONG`

### Security: Clarification User Validation

- [ ] `GetByClarificationMessageID` query JOINs `users` table to include `discord_id`
- [ ] `HandleGetByClarificationMessageID` response includes `discord_user_id` field (from users.discord_id)
- [ ] `MinionByClarificationResponse` struct in discord-bot includes `DiscordUserID *string` field
- [ ] `HandleReply` validates `minion.DiscordUserID != nil && *minion.DiscordUserID == m.Author.ID`
- [ ] Non-matching users receive warning message: "Only the original requester can answer clarification questions"
- [ ] Original requester's replies continue to work normally

### Context Handling: Timeout Contexts

- [ ] Constant `OperationTimeout = 30 * time.Second` defined in handler package
- [ ] `Handle` method uses `context.WithTimeout(context.Background(), OperationTimeout)` for orchestrator calls
- [ ] `HandleReply` method uses timeout context for orchestrator calls
- [ ] All timeout contexts have `defer cancel()` to prevent leaks

### Error Handling: Use errors.Is()

- [ ] Functions `isErrorType`, `containsError`, `containsSubstring` deleted from message.go
- [ ] `handleParseError` uses `errors.Is(err, command.ErrMissingRepo)` pattern
- [ ] All error type checks in message.go use standard library functions

### Devbox: Process Cleanup

- [ ] OpenCode PID captured when process starts (variable `OPENCODE_PID`)
- [ ] `kill "$OPENCODE_PID" 2>/dev/null || true` called when task times out
- [ ] Cleanup is best-effort (errors suppressed, doesn't affect exit code)

### Code Cleanup: Remove Dead Code

- [ ] `RateLimitError` struct deleted from `orchestrator/internal/api/minions.go`
- [ ] `IsRateLimitError` function deleted
- [ ] Custom `itoa` function deleted from `orchestrator/internal/db/minions.go`
- [ ] All `itoa` calls replaced with `strconv.Itoa()` (already imported)

### Code Cleanup: Extract Scan Helper

- [ ] `scanMinion(row pgx.Row) (*Minion, error)` helper function created
- [ ] Helper scans all 22 fields in correct order matching SELECT columns
- [ ] 9 full-minion queries refactored to use helper: `FindRecentDuplicate`, `GetByID`, `List`, `ListByStatuses`, `ListPending`, `ListIdleRunning`, `ListClarificationTimeouts`, `GetByClarificationMessageID`, `ListTerminalWithPodOlderThan`
- [ ] Partial-select queries (`Terminate`, `Complete`) remain inline (different column sets)

### Code Cleanup: Thinking Emoji Removal

- [ ] `Handle` adds `defer s.MessageReactionRemove(...)` after successful minion creation/clarification
- [ ] `HandleReply` adds defer after successful answer processing
- [ ] Emoji removal failures logged at Debug level (cosmetic, not Error)

---

## Testing Requirements

### Unit Tests

- [ ] Test `HandleCreate` rejects body > 1MB with 400
- [ ] Test `HandleCreate` rejects task > 10,000 chars with `TASK_TOO_LONG`
- [ ] Test `HandleGetByClarificationMessageID` returns `discord_user_id` from joined user
- [ ] Test `scanMinion` helper correctly populates all 22 fields

### Integration Tests

- [ ] Test clarification reply from wrong user is rejected with warning message
- [ ] Test clarification reply from original user succeeds
- [ ] Test context cancellation propagates to orchestrator client calls

### Manual Verification

- [ ] Verify thinking emoji removed after successful minion spawn
- [ ] Verify thinking emoji removed after failed minion spawn
- [ ] Verify devbox kills OpenCode on timeout (check pod logs)

---

## Technical Context

### Existing Patterns

- Request validation: `orchestrator/internal/api/minions.go:109-130` - validates required fields and formats
- Context usage: `orchestrator/internal/api/minions.go:321` - uses `r.Context()` for DB operations
- Error handling: `discord-bot/internal/handler/message.go:358` - uses `errors.Is()` for some checks

### Key Files

| File | Purpose |
|------|---------|
| `orchestrator/internal/api/minions.go` | API handlers needing body limits, task validation, response field addition |
| `orchestrator/internal/db/minions.go` | GetByClarificationMessageID needs JOIN, dead code removal, scan helper |
| `discord-bot/internal/handler/message.go` | Context fixes, error handling, emoji cleanup, user validation |
| `discord-bot/internal/orchestrator/client.go` | Response struct for clarification lookup |
| `devbox/entrypoint.sh` | Process cleanup on timeout |

### System Dependencies

- No new external dependencies
- No new package requirements
- No infrastructure changes required

### Data Model Changes

**None required.** The `discord_user_id` is stored in the `users` table (as `discord_id`), linked to minions via `user_id` FK. We will JOIN the `users` table in `GetByClarificationMessageID` to retrieve it—no schema migration needed.

**Database query change:**
```sql
-- Current (minions.go GetByClarificationMessageID)
SELECT ... FROM minions WHERE clarification_message_id = $1

-- New (with JOIN)
SELECT m.*, u.discord_id 
FROM minions m 
JOIN users u ON m.user_id = u.id 
WHERE m.clarification_message_id = $1
```

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Scan helper breaks field ordering | Low | High | Verify SELECT column order matches scan order; test coverage |
| Timeout context too short | Low | Med | 30s is generous; monitor for context deadline exceeded errors |
| Emoji removal fails silently | Low | Low | Log at Debug level; cosmetic issue only |
| Body limit breaks legitimate client | Low | Med | 1MB is 10x larger than any current task; rollback is revert + redeploy |
| JOIN query slower than single-table | Low | Low | Partial index on clarification_message_id already exists |

---

## Alternatives Considered

### Alternative 1: Configurable Body Limits

- **Description:** Make body limit configurable per-endpoint via config
- **Pros:** Flexibility for different payload sizes
- **Cons:** Unnecessary complexity; 1MB is sufficient for all current use cases
- **Decision:** Rejected. Use simple constant; can parameterize later if needed.

### Alternative 2: Permissive Clarification Validation

- **Description:** Allow any user to answer, but log warning
- **Pros:** More flexible for team scenarios
- **Cons:** Security risk; could allow unauthorized task modification
- **Decision:** Rejected. Strict validation preferred; can add "delegate" feature later if needed.

### Alternative 3: Remove Thinking Emoji Entirely

- **Description:** Don't add thinking emoji at all instead of adding/removing
- **Pros:** Simpler implementation
- **Cons:** Loses user feedback that bot is processing
- **Decision:** Rejected. Emoji provides valuable UX signal during processing.

### Alternative 4: Denormalize discord_user_id to Minions Table

- **Description:** Add migration to copy discord_id into minions table
- **Pros:** Simpler queries, no JOIN needed
- **Cons:** Data duplication, migration required, sync complexity
- **Decision:** Rejected. JOIN is simple and maintains normalization.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Connection epoch tracking for SSE**: Would prevent stale callbacks but adds complexity; current behavior is acceptable
- **SSE backoff persistence**: Backoff already resets per Connect() call; no change needed
- **Rate limiting for clarification replies**: Single reply per clarification is sufficient; spam protection deferred
- **Configurable task length per model**: 10k limit applies to all models; per-model limits deferred
- **Git credential cleanup trap in devbox**: Pods are ephemeral; credential cleanup is nice-to-have
- **Metrics for security violations**: Accept manual log review for v1; add metrics if abuse detected
- **Configurable operation timeout**: 30s fixed for now; make configurable if monitoring shows issues

---

## Documentation Requirements

- [ ] No user-facing documentation updates required
- [ ] No API documentation updates required (internal changes only)
- [ ] No runbook updates required

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| None | - | - | - |

All questions resolved during planning phase.

---

## Suggested PR Strategy

To minimize risk and enable incremental review:

| PR | Scope | Effort | Risk |
|----|-------|--------|------|
| PR1 | Security: body limits, task length, user validation | M | Low |
| PR2 | Code cleanup: dead code removal, scan helper | M | Medium |
| PR3 | Context handling, error handling, emoji removal | S | Low |
| PR4 | Devbox: process cleanup on timeout | S | Low |

**Total estimated effort:** 8-12 hours

---

## Appendix

### Glossary

- **SSE**: Server-Sent Events - HTTP-based streaming for pod event delivery
- **Clarification**: Pre-flight question asked by LLM before spawning a minion
- **MaxBytesReader**: Go stdlib wrapper that limits request body size
- **JOIN**: SQL operation to combine rows from two tables based on related column

### References

- Code review that identified these issues (this conversation)
- Existing migration: `schema/migrations/004_create_indexes.sql` (clarification_message_id index)
- Minion schema: `schema/migrations/002_create_minions_table.sql`
- Users schema: `schema/migrations/001_create_users_table.sql`
