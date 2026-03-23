# PRD: Chat View Event Filtering & Status Improvements

**Date:** 2026-03-23

---

## Problem Statement

### What problem are we solving?

The control panel's chat view displays raw JSON for internal metadata events that should be hidden, creating a confusing user experience. Users see spam like:

```
11:58:03
{"file":"/workspace/repo/cmd/server.go"}
11:58:03
{"info":{"agent":"build","cost":0.0017057,...}}
11:58:03
{"sessionID":"ses_2e3f1ef0cffeI1TYbJUrQdhLE2","status":{"type":"busy"}}
```

Additionally:
1. **Missing tool inputs**: Some tool calls (e.g., `apply_patch`) show empty `{}` for input despite clearly having made changes
2. **No agent activity indicator**: Users can't tell if the agent is actively thinking or idle
3. **No debugging capability**: When unknown event formats appear, there's no way to inspect the raw SSE data

### Why now?

This is a polish pass on the recently implemented chat view. The core functionality works but the UX needs refinement before broader use.

### Who is affected?

- **Primary users:** Developers monitoring minion activity in the control panel
- **Secondary users:** Developers debugging event parsing issues

---

## Proposed Solution

### Overview

Three improvements to the chat view:

1. **Filter internal metadata events** - Skip rendering events that are internal bookkeeping (file changes, session info, diffs, status updates) rather than user-facing content
2. **Add session status bar** - Show a compact indicator at the top of the chat view displaying the agent's current state (busy/idle/retry/completed/failed)
3. **Add debug logging** - Environment-variable controlled logging of raw SSE events to browser console for debugging unknown tool formats

### User Experience

#### User Flow: Normal Monitoring
1. User opens minion detail page
2. Status bar shows "Agent is thinking..." with pulsing indicator while agent is working
3. Chat view shows clean messages: text, tool calls, thinking blocks (no raw JSON spam)
4. When agent finishes, status bar shows "Completed" or "Failed"

#### User Flow: Debugging Unknown Events
1. Developer sets `NEXT_PUBLIC_DEBUG_SSE=true` in `.env.local`
2. Developer restarts dev server
3. Developer opens browser console while monitoring minion
4. Console shows `[SSE] Raw event: {...}` for each event
5. Developer identifies the structure of unknown tools/events

---

## End State

When this PRD is complete, the following will be true:

- [ ] Internal metadata events (file changes, session info, diffs, status updates) are filtered from chat view
- [ ] Session status bar displays at top of chat view showing busy/idle/retry state
- [ ] Status bar shows final state (Completed/Failed/Terminated) for terminal minions
- [ ] `NEXT_PUBLIC_DEBUG_SSE=true` environment variable enables raw SSE logging to console
- [ ] Debug logs include both raw and parsed event structures
- [ ] All existing chat functionality (text, tools, thinking, subtasks) continues to work

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Raw JSON events visible | Many | 0 | Manual testing |
| Debug time for unknown tools | Hours | Minutes | Self-tracking |

### Qualitative

- Chat view feels clean and professional
- Users can understand agent activity at a glance
- Developers can quickly diagnose event parsing issues

---

## Acceptance Criteria

### Feature: Internal Event Filtering

- [ ] Events with `content.file` (string) and ≤2 keys are skipped
- [ ] Events with `content.info` (object) are skipped
- [ ] Events with `content.diff` (array) are skipped
- [ ] Events with `content.sessionID` + `content.status` (no messageID) are skipped
- [ ] Events with `content.sessionID` + `content.summary` are skipped
- [ ] Existing skip logic (heartbeat, step-start, step-finish, snapshot, patch, compaction) still works
- [ ] Legitimate events (text, tool, reasoning, agent, retry, error) still render correctly

### Feature: Session Status Bar

- [ ] Status bar appears at top of chat view (inside the border, above messages)
- [ ] Shows "Agent is thinking..." with blue pulsing dot when `sessionState === "busy"`
- [ ] Shows "Idle" with gray dot when `sessionState === "idle"`
- [ ] Shows "Retrying..." with yellow dot when `sessionState === "retry"`
- [ ] Shows "Completed" with green dot for completed minions
- [ ] Shows "Failed" with red dot for failed minions
- [ ] Shows "Terminated" with orange dot for terminated minions
- [ ] Status bar hidden when no status information available (pending minions before first event)

### Feature: Debug Logging

- [ ] `NEXT_PUBLIC_DEBUG_SSE` environment variable controls logging
- [ ] When `"true"`, logs `[SSE] Raw event: <JSON>` for each SSE message
- [ ] When `"true"`, logs `[SSE] Parsed event: <JSON>` showing normalized structure
- [ ] When unset or `"false"`, no debug logging occurs
- [ ] Logging does not break event processing

---

## Technical Context

### Existing Patterns

- Event filtering: `control-panel/src/lib/event-aggregation.ts:220-227` - `shouldSkipEvent()` function
- Event type detection: `control-panel/src/lib/event-aggregation.ts:91-102` - `getPartType()` function
- SSE parsing: `control-panel/src/hooks/use-minion-events.ts:185-204` - `onmessage` handler

### Key Files

| File | Relevance |
|------|-----------|
| `control-panel/src/lib/event-aggregation.ts` | Main event filtering and aggregation logic |
| `control-panel/src/hooks/use-minion-events.ts` | SSE connection and event parsing |
| `control-panel/src/components/chat-view.tsx` | Chat UI, will host status bar |
| `control-panel/src/components/system-message-row.tsx` | Currently renders unknown events as JSON |
| `control-panel/src/types/minion.ts` | Type definitions |

### System Dependencies

- Next.js environment variables (must use `NEXT_PUBLIC_` prefix for client-side)
- No external service dependencies
- No database changes

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Over-filtering legitimate events | Medium | High | Conservative detection patterns; only skip events with specific structural signatures |
| Status bar flickers during rapid updates | Low | Low | Status extracted from latest event only; no rapid state machine |
| Debug logging left on in production | Low | Low | Environment variable pattern ensures explicit opt-in |

---

## Alternatives Considered

### Alternative 1: Whitelist Instead of Blacklist

- **Description:** Only render known event types, skip everything else
- **Pros:** Simpler logic, no unknown events leak through
- **Cons:** New legitimate event types would be hidden until explicitly added
- **Decision:** Rejected. Blacklist approach is safer for forward compatibility.

### Alternative 2: Collapsible "Debug Events" Section

- **Description:** Show all events but collapse internal ones into an expandable section
- **Pros:** Nothing hidden, power users can inspect everything
- **Cons:** Still clutters UI; adds complexity; most users don't want to see this
- **Decision:** Rejected. Clean UI is higher priority; debug logging serves power user needs.

### Alternative 3: Status in Header Instead of Chat View

- **Description:** Put busy/idle indicator in the minion detail header
- **Pros:** More prominent, visible without scrolling
- **Cons:** Disconnected from chat context; header already has status badge
- **Decision:** Rejected. Status bar at top of chat view provides context-appropriate feedback.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Fixing `apply_patch` input extraction** - Deferred until we collect debug data to understand the event structure
- **Real-time token/cost updates in chat** - The header already shows this from database; live updates would require additional aggregation
- **Filtering by event type UI** - No user-facing filter controls; filtering is automatic
- **Persistent debug log storage** - Console logging only; no file or remote logging

---

## Interface Specifications

### Environment Variables

```
NEXT_PUBLIC_DEBUG_SSE=true|false

When true:
- Console logs: [SSE] Raw event: {"type":"...","properties":{...}}
- Console logs: [SSE] Parsed event: {"id":"...","event_type":"...","content":{...}}
```

### UI: Status Bar

```
┌─────────────────────────────────────────────────┐
│ ● Agent is thinking...                          │  <- Blue pulsing dot, blue background
├─────────────────────────────────────────────────┤
│ Chat messages...                                │
│                                                 │
└─────────────────────────────────────────────────┘

States:
- Busy:       ● (pulsing blue)  "Agent is thinking..."   bg-blue-900/30
- Idle:       ● (gray)          "Idle"                   bg-gray-800/50
- Retry:      ● (yellow)        "Retrying..."            bg-yellow-900/30
- Completed:  ● (green)         "Completed"              bg-green-900/30
- Failed:     ● (red)           "Failed"                 bg-red-900/30
- Terminated: ● (orange)        "Terminated"             bg-orange-900/30
```

---

## Documentation Requirements

- [ ] No user-facing documentation needed (internal tool)
- [ ] Update AGENTS.md if debugging instructions are helpful for future agents

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| What is the `apply_patch` event structure? | - | After debug logging | Open |
| Should we add `apply_patch` to tool summaries? | - | After structure known | Blocked |

---

## Appendix

### Event Types to Skip

Based on analysis of SSE stream, these internal events should be filtered:

| Event Pattern | Example | Detection Logic |
|--------------|---------|-----------------|
| File change | `{"file":"/path/file.go"}` | `content.file` is string, ≤2 keys |
| File watcher | `{"event":"change","file":"..."}` | Same as above |
| Session info | `{"info":{...tokens, cost...}}` | `content.info` is object |
| Session diff | `{"diff":[...],"sessionID":"..."}` | `content.diff` is array |
| Bare status | `{"sessionID":"...","status":{...}}` | Both keys, no messageID |
| Summary | `{"sessionID":"...","summary":{...}}` | Both keys present |

### References

- Original chat view PRD: `.opencode/state/chat-view/prd.md`
- Minions system PRD: `.opencode/state/minions-system/prd.md`
- OpenCode event types research: Conducted during planning phase
