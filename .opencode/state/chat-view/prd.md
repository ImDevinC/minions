# PRD: Chat View for Minion Event Log

**Date:** 2026-03-23

---

## Problem Statement

### What problem are we solving?

The control panel's minion detail page displays agent activity as a raw event log. Users see individual events like `message.part.delta`, `message.part.updated`, `tool`, etc. that require manual expansion to understand. This creates several UX problems:

1. **Cognitive overload**: Users must mentally reconstruct the agent's conversation from fragmented streaming events
2. **Noise**: Many events show `null` content or low-value metadata
3. **Inaccessibility**: Non-technical users cannot understand what the agent is doing
4. **Friction**: Every interaction requires expanding collapsed JSON blobs

### Why now?

The minion system is working (pods spawn, SSE streams, events persist), but the observability UX undermines trust. Users cannot easily verify what the agent did, making them hesitant to use minions for real work.

### Who is affected?

- **Primary users:** Developers monitoring their minion's progress and reviewing output
- **Secondary users:** Team leads auditing minion activity for quality/cost

---

## Proposed Solution

### Overview

Replace the raw event log with a ChatGPT-style chat view that renders the agent's output as a readable conversation. Text content appears as formatted markdown messages, tool calls appear as compact collapsible cards, and reasoning/thinking content appears in collapsed "Thinking..." blocks.

### User Experience

Users navigate to a minion detail page (`/minions/:id`) and see the agent's activity rendered as a chat transcript. As the agent works, new content streams in at the bottom with auto-scroll.

#### User Flow: Monitoring Active Minion

1. User opens minion detail page
2. Chat view shows accumulated agent output with latest at bottom
3. As agent streams text, it appears in real-time (like ChatGPT typing)
4. When agent uses a tool, a compact card appears (e.g., "📖 Read • src/foo.ts")
5. User clicks tool card to expand and see input/output details
6. Thinking blocks show "▶ Thinking..." collapsed, expandable if curious
7. Auto-scroll keeps user at bottom; scroll up pauses auto-scroll with "Jump to latest" button

#### User Flow: Reviewing Completed Minion

1. User opens minion detail page for a completed/failed minion
2. Full chat transcript loads showing everything the agent did
3. User scrolls through to understand what happened
4. Tool cards and thinking blocks are collapsed by default for quick scanning

### Design Considerations

- **Markdown rendering**: Full markdown with syntax-highlighted code blocks (using existing `react-syntax-highlighter`)
- **Tool cards**: Compact single-line cards showing icon + tool name + status + summary. Expandable for full details.
- **Thinking blocks**: Collapsed by default with "▶ Thinking..." header. Shows reasoning text when expanded.
- **Auto-scroll**: Enabled by default, disabled when user scrolls up, "Jump to latest" button to re-enable
- **Performance**: Virtualized list for smooth scrolling (hundreds of messages)

---

## End State

When this PRD is complete, the following will be true:

- [ ] Minion detail page shows chat view instead of raw event log
- [ ] Text content renders as markdown with syntax-highlighted code blocks
- [ ] Tool calls render as compact collapsible cards with icon, name, status, and summary
- [ ] Reasoning content renders in collapsible "Thinking..." blocks
- [ ] Subtask spawns render as nested conversation threads (collapsed by default)
- [ ] Real-time streaming updates work (new content appears as agent works)
- [ ] Delta events accumulate correctly (streaming text appends, not replaces)
- [ ] Auto-scroll behavior matches current event log
- [ ] Error states handled gracefully (connection loss, malformed events, session errors)
- [ ] Loading and empty states provide clear feedback
- [ ] Performance handles hundreds of messages smoothly
- [ ] Basic responsive layout works on tablet-sized screens (768px+)

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Time to understand agent output | Minutes (manual expansion) | Seconds (glance) | User feedback |
| Event log render performance | Smooth (hundreds) | Same or better | Manual testing |

### Qualitative

- Users report the control panel "makes sense" without technical SSE knowledge
- Users can quickly answer "what did the agent do?" from the chat view

---

## Acceptance Criteria

### Chat Message Rendering

- [ ] `TextPart` events render as markdown paragraphs with proper formatting
- [ ] Code blocks in text render with syntax highlighting (language detection)
- [ ] Multiple sequential text parts within a message concatenate cleanly
- [ ] Empty or null content does not render (no blank bubbles)

### Tool Call Cards

- [ ] `ToolPart` events render as compact single-line cards
- [ ] Card shows: icon (based on tool type), tool name, status badge, one-line summary
- [ ] Status badge colors: pending (gray), running (blue pulse), completed (green), error (red)
- [ ] Clicking card expands to show full input and output
- [ ] Tool summaries are human-readable (e.g., "Read src/foo.ts", "Found 12 files", "Edited 3 lines")

### Thinking/Reasoning Blocks

- [ ] `ReasoningPart` events render as collapsible blocks
- [ ] Default state is collapsed showing "▶ Thinking..."
- [ ] Clicking expands to show full reasoning text
- [ ] Collapsed state shows subtle styling (muted, smaller) to not distract

### Streaming & Auto-scroll

- [ ] New content appears in real-time as SSE events arrive
- [ ] Auto-scroll keeps view at bottom during streaming
- [ ] Scrolling up disables auto-scroll
- [ ] "Jump to latest" button appears when auto-scroll disabled
- [ ] Clicking "Jump to latest" re-enables auto-scroll and scrolls to bottom

### Event Aggregation

- [ ] Events are grouped by `messageID` into coherent chat messages
- [ ] Event types not relevant to chat view are filtered (e.g., `server.heartbeat`)
- [ ] `step-finish` events are handled gracefully (end of assistant turn)
- [ ] Duplicate events (same ID) are deduplicated
- [ ] Events are sorted by `timestamp` field (not arrival order) to handle out-of-order delivery

### Delta & Streaming Behavior

- [ ] Delta events (`properties.delta` present) append to existing text, not replace
- [ ] Non-delta updates replace part content entirely
- [ ] Rapid delta events batch into single React render (debounce ~16ms)
- [ ] Streaming indicator shows while message is still receiving events

### Error Handling

- [ ] `session.error` events render as error banners in chat (red background)
- [ ] Malformed event JSON logs to console but doesn't crash UI
- [ ] Missing `messageID` events render as system messages outside conversation flow
- [ ] SSE connection loss shows existing "Reconnecting..." indicator
- [ ] Unknown event types log warning and render as subtle system message
- [ ] `retry` part events render as warning banners ("⚠️ Retrying, attempt 2/3")

### Loading & Empty States

- [ ] Initial page load shows skeleton loaders for chat area
- [ ] "No events yet" state shows when minion hasn't produced output
- [ ] Running minion with no events shows "Waiting for agent to start..." with pulse animation
- [ ] Reconnection after SSE drop shows "Catching up..." indicator briefly
- [ ] Completed minion with zero events shows "No output produced" message

### Subtask Rendering

- [ ] `subtask` parts render as nested conversation threads
- [ ] Nested threads are collapsed by default with "▶ Subtask: [description]" header
- [ ] Clicking expands to show the subtask's messages inline (indented)
- [ ] Subtask thread has subtle visual distinction (left border or background)

### Memory & Performance

- [ ] Expanding a tool card auto-collapses previously expanded cards (accordion behavior)
- [ ] Tool outputs >10KB are truncated with "Show full output" button
- [ ] Markdown parsing results are memoized by part ID
- [ ] Virtualization window renders ~20 items above/below viewport

### Responsive Layout

- [ ] Chat view is usable on tablet screens (768px+)
- [ ] Tool cards remain readable on narrower screens
- [ ] Code blocks scroll horizontally (no forced line wrap)
- [ ] "Jump to latest" button repositions appropriately on smaller screens

---

## Technical Context

### Existing Patterns

- **Virtualized list**: `control-panel/src/components/minion-detail-client.tsx` uses `@tanstack/react-virtual` for efficient rendering
- **Syntax highlighting**: `react-syntax-highlighter` with `oneDark` theme already used for code blocks
- **SSE hook**: `control-panel/src/hooks/use-minion-events.ts` handles EventSource connection, reconnection, and event deduplication
- **Event structure**: Events have `{ id, timestamp, event_type, content }` shape

### Key Files

- `control-panel/src/components/minion-detail-client.tsx` - Current EventLog and EventRow components (to be replaced)
- `control-panel/src/hooks/use-minion-events.ts` - SSE event hook (unchanged, provides raw events)
- `control-panel/src/types/minion.ts` - TypeScript types for MinionEvent

### OpenCode Event Types (from SSE)

Events follow OpenCode's event bus format. Key event types to handle:

| Event Type | Part Type | Chat Representation |
|------------|-----------|---------------------|
| `message.part.updated` | `text` | Markdown text content |
| `message.part.updated` | `reasoning` | Collapsible thinking block |
| `message.part.updated` | `tool` | Tool call card |
| `message.part.updated` | `subtask` | Nested conversation thread (collapsed) |
| `message.part.updated` | `retry` | Warning banner ("⚠️ Retrying, attempt N") |
| `message.part.updated` | `agent` | System message ("Switched to `X` agent") |
| `message.part.updated` | `step-start` | (Internal: start of turn, skip rendering) |
| `message.part.updated` | `step-finish` | (Internal: end of turn, has token counts) |
| `message.part.updated` | `snapshot` | (Internal: state snapshot, skip rendering) |
| `message.part.updated` | `patch` | (Internal: file changes, skip rendering) |
| `message.part.updated` | `compaction` | (Internal: cleanup event, skip rendering) |
| `message.part.updated` | `file` | (Deferred: file attachment placeholder) |
| `message.updated` | - | Message metadata update |
| `session.error` | - | Error banner in chat (red, shows details) |
| `session.status` | - | Status indicator if in retry state |
| `error` | - | Error banner in chat |

### Part Structure (from event.content)

```typescript
// TextPart
{ type: "text", id, sessionID, messageID, text: string }

// ReasoningPart  
{ type: "reasoning", id, sessionID, messageID, text: string }

// ToolPart
{ type: "tool", id, sessionID, messageID, tool: string, state: ToolState }

// ToolState variants
{ status: "pending", input: object }
{ status: "running", input: object, title?: string }
{ status: "completed", input: object, output: string, title: string }
{ status: "error", input: object, error: string }
```

### Delta Accumulation Strategy

For `message.part.updated` events with text content:

- If `properties.delta` field exists: Append delta to existing text buffer for that part ID
- If `properties.delta` is absent: Replace part content entirely with `properties.part.text`
- Part identity: Use `part.id` for tracking accumulation state
- Implementation: Maintain `Map<partId, accumulatedText>` in aggregation logic

```typescript
// Pseudocode for delta handling
function handlePartUpdate(event: MessagePartUpdated, state: Map<string, string>) {
  const partId = event.properties.part.id;
  
  if (event.properties.delta !== undefined) {
    // Streaming delta - append to existing
    const existing = state.get(partId) || '';
    state.set(partId, existing + event.properties.delta);
  } else {
    // Full update - replace entirely
    state.set(partId, event.properties.part.text || '');
  }
}
```

### System Dependencies

- **react-syntax-highlighter**: Already in package.json, use for code blocks within markdown
- **@tanstack/react-virtual**: Already in package.json, use for virtualization
- **react-markdown** (^9.0.0): New dependency for markdown rendering with GFM support
- **remark-gfm** (^4.0.0): New dependency for GitHub Flavored Markdown (tables, strikethrough, etc.)

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Event format changes in OpenCode | Low | High | Defensive parsing with fallbacks for unknown event types |
| Performance regression with markdown rendering | Low | Med | Virtualization ensures only visible items render; memoize parsed markdown |
| Tool summary generation complexity | Med | Low | Start with simple summaries (tool name + status), iterate based on feedback |
| Incomplete event type coverage | Med | Med | Log unknown event types, render as subtle "system" messages rather than crashing |
| Delta accumulation bugs (duplicates, gaps) | Med | High | Unit tests for delta append logic; integration test with real SSE stream |
| XSS via markdown content | Low | High | react-markdown auto-sanitizes; avoid dangerouslySetInnerHTML |
| Virtualization breaks auto-scroll on re-measure | Low | Med | Test scroll-to-bottom after content height changes from expansion |

---

## Alternatives Considered

### Alternative 1: Add Tab Toggle (Chat View / Raw Events)

- **Description:** Keep both views, let users switch between them
- **Pros:** Power users can still see raw events for debugging
- **Cons:** More UI complexity, maintenance burden, unclear which is "default"
- **Decision:** Rejected for v1. If debugging need arises, users can check browser devtools or DB directly.

### Alternative 2: Hybrid View (Chat with inline raw JSON expanders)

- **Description:** Chat view but every message has "View raw" expander
- **Pros:** Single view serves both needs
- **Cons:** Clutters the clean chat UX, most users never need raw view
- **Decision:** Rejected. Tool card expansion already shows relevant details.

### Alternative 3: Polling-based updates instead of SSE

- **Description:** Fetch events via REST polling instead of streaming
- **Pros:** Simpler connection management
- **Cons:** Latency, higher server load, worse UX for real-time monitoring
- **Decision:** Rejected. SSE already works and provides better UX.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Reply/interact with agent**: View-only for now. Future PRD may add clarification responses.
- **Search/filter chat history**: Scroll is sufficient for hundreds of messages. Add search if sessions get very long.
- **Export chat transcript**: Not requested. Can add later if useful.
- **Multiple chat sessions**: Minions are 1:1 with sessions. No session switching needed.
- **User messages display**: Minions don't have back-and-forth; agent runs task autonomously. Only show agent output.
- **Customizable tool icons/colors**: Hardcode sensible defaults. Customize later if needed.
- **File attachment preview**: Tool outputs with `file` parts render as "File attachment" placeholder. Full preview (images, PDFs) deferred to v2.
- **Full mobile optimization**: Tablet support (768px+) only. Phone-optimized layout deferred to v2.
- **WCAG accessibility**: Screen reader announcements, keyboard navigation for expand/collapse deferred. Add ARIA labels in v2 if compliance required.
- **Permission prompt UI**: If permission events appear, render as system message. Interactive permission approval deferred.

---

## Interface Specifications

### UI Components

#### ChatView

Container component replacing EventLog. Responsibilities:
- Receive raw `MinionEvent[]` from `useMinionEvents` hook
- Transform events into `ChatMessage[]` via aggregation logic
- Render virtualized list of ChatMessage components
- Handle auto-scroll behavior

#### ChatMessage

Renders a single coherent assistant output. Contains:
- Optional ThinkingBlock (collapsed reasoning)
- Text content (markdown rendered)
- Tool cards (in order they appeared)

#### ToolCallCard

Compact expandable card for tool invocations:
- Collapsed: `[icon] [tool name] • [summary] [status badge] [▼]`
- Expanded: Input params + output/error

#### ThinkingBlock

Collapsed reasoning content:
- Collapsed: `▶ Thinking...`
- Expanded: Full reasoning text

### Event → ChatMessage Transformation

```typescript
interface ChatMessage {
  id: string;                    // First event's messageID
  timestamp: string;             // Earliest timestamp in group
  thinking?: string;             // Accumulated reasoning text
  text: string;                  // Accumulated text content
  tools: ToolCall[];             // Tool parts in order
  isStreaming: boolean;          // True if message still receiving events
}

interface ToolCall {
  id: string;
  tool: string;
  status: "pending" | "running" | "completed" | "error";
  title?: string;
  summary: string;               // Generated human-readable summary
  input: Record<string, unknown>;
  output?: string;
  error?: string;
}
```

### Tool Summary Generation

Tool summaries extract human-readable descriptions from tool input/output:

```typescript
function getToolSummary(tool: string, input: Record<string, unknown>, output?: string): string {
  switch (tool.toLowerCase()) {
    case 'read':
      return `Read ${input.filePath || input.path || 'file'}`;
    case 'write':
      return `Wrote ${input.filePath || input.path || 'file'}`;
    case 'edit':
      return `Edited ${input.filePath || input.path || 'file'}`;
    case 'glob': {
      const files = output?.split('\n').filter(Boolean) || [];
      return `Found ${files.length} file${files.length !== 1 ? 's' : ''}`;
    }
    case 'grep': {
      const matches = output?.split('\n').filter(Boolean) || [];
      return `Found ${matches.length} match${matches.length !== 1 ? 'es' : ''}`;
    }
    case 'bash': {
      const cmd = String(input.command || '').slice(0, 40);
      return `Ran: ${cmd}${String(input.command || '').length > 40 ? '...' : ''}`;
    }
    case 'task':
      return `Spawned ${input.subagent_type || 'sub'}agent`;
    case 'webfetch':
      return `Fetched ${input.url || 'URL'}`;
    case 'todowrite':
      return 'Updated todo list';
    case 'question':
      return 'Asked clarifying question';
    default:
      return tool; // Fallback to tool name
  }
}
```

---

## Documentation Requirements

- [ ] No user-facing documentation needed (internal tool)
- [ ] Update AGENTS.md if architecture section mentions event log

---

## Resolved Questions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Markdown library | `react-markdown` + `remark-gfm` | Full GFM support (tables, strikethrough), auto-sanitizes XSS, ~70KB acceptable for control panel |

---

## Appendix

### Glossary

- **SSE (Server-Sent Events)**: HTTP streaming protocol for real-time updates from server to client
- **OpenCode**: AI coding agent CLI that minion pods run
- **Part**: A discrete piece of content within a message (text, tool call, reasoning, etc.)
- **Virtualization**: Rendering only visible list items for performance

### References

- OpenCode Server Docs: https://opencode.ai/docs/server/
- OpenCode SDK Types: https://github.com/anomalyco/opencode/blob/dev/packages/sdk/js/src/gen/types.gen.ts
- Current EventLog implementation: `control-panel/src/components/minion-detail-client.tsx`

### Visual Reference

```
┌─────────────────────────────────────────────────────────┐
│ ▶ Thinking...                                           │
├─────────────────────────────────────────────────────────┤
│                                                         │
│ I'll help you implement the feature. Let me first       │
│ explore the codebase to understand the structure.       │
│                                                         │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ 🔍 Glob  • Found 12 files matching **/*.ts    ✓    │ │
│ └─────────────────────────────────────────────────────┘ │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ 📖 Read  • src/components/Button.tsx          ✓    │ │
│ └─────────────────────────────────────────────────────┘ │
│                                                         │
│ Based on the existing patterns, I'll create a new       │
│ component that follows your conventions:                │
│                                                         │
│ ```tsx                                                  │
│ export function NewComponent() {                        │
│   return <div>Hello</div>;                              │
│ }                                                       │
│ ```                                                     │
│                                                         │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ ✏️ Write • src/components/NewComponent.tsx    ✓    │ │
│ └─────────────────────────────────────────────────────┘ │
│                                                         │
│ Done! I've created the new component.                   │
│                                                         │
└─────────────────────────────────────────────────────────┘
                                          [↓ Jump to latest]
```
