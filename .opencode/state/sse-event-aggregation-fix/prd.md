# PRD: SSE Event Aggregation Fix

**Date:** 2026-03-23

---

## Problem Statement

### What problem are we solving?

The control panel's SSE event aggregation has two bugs that cause empty content in the UI:

1. **Thinking/reasoning blocks are empty** - Users see collapsed thinking blocks but no actual reasoning text inside them
2. **Tool input sections are empty** - Tool call cards show the tool name and output, but input parameters are missing

This breaks observability for users monitoring minion execution. They can't see:
- What the AI is thinking/reasoning about
- What parameters were passed to tools (file paths, search patterns, etc.)

### Why now?

These are functional bugs blocking production use of the control panel. Users can't effectively monitor or debug minion behavior without this information.

### Who is affected?

- **Primary users:** Developers monitoring minion execution via the control panel
- **Secondary users:** Anyone debugging minion behavior or reviewing task execution history

---

## Proposed Solution

### Overview

Fix the event aggregation logic in `event-aggregation.ts` to properly handle:
1. `message.part.delta` events that stream reasoning/thinking content incrementally
2. Tool input merging when tool state updates arrive with non-empty input

### Technical Details

**Bug 1 - Delta Events Not Processed:**
- `message.part.updated` events create reasoning parts with empty `text: ""`
- `message.part.delta` events stream actual content with flat structure: `{delta, partID, field}`
- Current `extractTextContent()` only handles `part.updated` events (checks `part.type`)
- Delta events have completely different structure and are ignored

**Bug 2 - Tool Input Not Merged:**
- Tool events arrive in phases: first with `input: {}` (pending), then with actual input (running)
- Current merge logic updates `status`, `output`, `error`, `title` but skips `input`
- When second event arrives with real input, it's discarded

---

## End State

When this PRD is complete, the following will be true:

- [ ] Thinking/reasoning blocks display actual reasoning content from delta events
- [ ] Tool call cards display input parameters (file paths, search patterns, etc.)
- [ ] Delta events are accumulated by partID and routed to correct content type (text vs reasoning)
- [ ] Part types are tracked from `message.part.updated` events for delta classification
- [ ] Tool input is merged when non-empty input arrives in subsequent events
- [ ] Existing functionality (text content, tool output, streaming state) remains intact

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Thinking content visible | 0% | 100% | Manual verification with SSE test file |
| Tool input visible | 0% | 100% | Manual verification with SSE test file |

### Qualitative

- Users can see AI reasoning in thinking blocks
- Users can see tool parameters in tool call cards

---

## Acceptance Criteria

### Feature: Delta Event Processing
- [ ] New `extractDeltaContent()` function handles flat delta structure `{delta, partID, field}`
- [ ] `DeltaState` interface includes `partTypeByID: Map<string, "text" | "reasoning">`
- [ ] Part types tracked from `message.part.updated` events
- [ ] `message.part.delta` events accumulated by partID into `textByPart`
- [ ] Deltas routed to `thinking` or `textContent` based on tracked part type
- [ ] Unknown part types default to "text" (with optional warning log)

### Feature: Tool Input Merge
- [ ] Tool merge logic includes input: `if (toolCall.input && Object.keys(toolCall.input).length > 0) { existing.input = toolCall.input; }`
- [ ] Empty input `{}` does not overwrite existing input
- [ ] Non-empty input replaces previous input (not deep merge)

---

## Technical Context

### Existing Patterns

- `extractTextContent()` at `event-aggregation.ts:372-394` - Pattern for extracting content from events with nested `part` structure
- `extractToolCall()` at `event-aggregation.ts:335-366` - Pattern for extracting tool data from events
- Tool merge logic at `event-aggregation.ts:629-639` - Existing merge pattern to extend

### Key Files

- `control-panel/src/lib/event-aggregation.ts` - Main file to modify, contains all aggregation logic
- `sse` - Sample SSE event data (8949 lines) showing event structures
- `control-panel/src/types/minion.ts` - Type definitions including `ChatMessage.thinking`
- `control-panel/src/components/thinking-block.tsx` - Renders thinking content (consumer)
- `control-panel/src/components/tool-call-card.tsx` - Renders tool calls with input/output (consumer)

### Key Code Locations

| Location | Description |
|----------|-------------|
| Lines 33-38 | `DeltaState` interface - add `partTypeByID` |
| Lines 372-394 | `extractTextContent()` - reference for new function |
| Lines 596-622 | Text/reasoning extraction loop - add delta handling |
| Lines 625-641 | Tool call processing - add input merge |
| Line 693 | `createDeltaState()` - initialize new Map |

### SSE Event Structures

**message.part.updated (creates part with type):**
```json
{
  "event_type": "message.part.updated",
  "content": {
    "part": {
      "type": "reasoning",
      "text": "",
      "id": "prt_..."
    }
  }
}
```

**message.part.delta (streams content - FLAT structure):**
```json
{
  "event_type": "message.part.delta",
  "content": {
    "delta": "**Analyzing the code...",
    "field": "text",
    "partID": "prt_...",
    "messageID": "msg_..."
  }
}
```

**Tool events (input arrives in phases):**
```json
// First event (pending)
{"state": {"input": {}, "status": "pending"}}

// Second event (running)  
{"state": {"input": {"path": "", "pattern": "redis"}, "status": "running"}}
```

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Delta arrives before part.updated | Low | Low | Default to "text" type, add warning log |
| Breaking existing text/reasoning flow | Med | High | Careful testing with SSE file, preserve existing logic paths |
| Performance regression with new Map | Low | Low | Map operations are O(1), minimal overhead |

---

## Alternatives Considered

### Alternative 1: Modify extractTextContent() to handle deltas

- **Description:** Add branching to existing function to detect delta structure
- **Pros:** Single function, less code
- **Cons:** Mixes two different event structures, harder to maintain, function becomes complex
- **Decision:** Rejected. Separate `extractDeltaContent()` is cleaner and more maintainable.

### Alternative 2: Deep merge for tool input

- **Description:** Use `{...existing.input, ...toolCall.input}` instead of replace
- **Pros:** Would handle incremental input updates
- **Cons:** SSE data shows inputs are either empty or complete, not incremental
- **Decision:** Rejected. Replace semantics match actual event behavior.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- Performance optimization of aggregation loop - not needed, current perf is fine
- Adding observability/logging for event processing - can add later if debugging needed
- Handling hypothetical future event types - solve when they appear
- Unit tests for new functions - manual verification sufficient for bugfix

---

## Open Questions

| Question | Owner | Due Date | Status |
|----------|-------|----------|--------|
| None | - | - | - |

All questions resolved during analysis phase.

---

## Appendix

### Glossary

- **SSE:** Server-Sent Events - streaming protocol for real-time updates
- **Delta event:** Incremental content update (just the new text chunk)
- **Part:** A discrete section of a message (text, reasoning, tool call)
- **partID:** Unique identifier for a message part, used to correlate updates

### References

- Oracle review confirmed analysis and approach
- SSE event file at `/home/devin/Projects/minions/sse` contains real event examples
