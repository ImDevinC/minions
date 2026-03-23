/**
 * Event aggregation logic for transforming raw MinionEvents into ChatMessages.
 *
 * Groups events by messageID, sorts by timestamp, deduplicates, filters heartbeats,
 * and produces a list of ChatMessages and SystemMessages ready for rendering.
 *
 * Delta streaming: Events with `properties.delta` append to existing text buffers.
 * Non-delta events replace the content entirely.
 */

import type {
  MinionEvent,
  ChatMessage,
  SystemMessage,
  ToolCall,
  ToolCallStatus,
  SubtaskThread,
  TextPart,
} from "@/types/minion";

/**
 * Result of aggregating events.
 */
export interface AggregationResult {
  messages: ChatMessage[];
  systemMessages: SystemMessage[];
}

/**
 * Internal state for delta accumulation.
 * Tracks accumulated text per partID and which events have been processed.
 */
export interface DeltaState {
  /** Maps partID -> accumulated text content */
  textByPart: Map<string, string>;
  /** Set of event IDs that have already been processed (for delta deduplication) */
  processedEventIds: Set<string>;
}

/**
 * Part types that should be skipped (internal events).
 */
const SKIP_PART_TYPES = new Set([
  "step-start",
  "step-finish",
  "snapshot",
  "patch",
  "compaction",
]);

/**
 * Part types that should be rendered as system messages rather than chat messages.
 */
const SYSTEM_PART_TYPES = new Set(["agent", "retry"]);

/**
 * Event types that are heartbeats (should be filtered out).
 */
function isHeartbeatEvent(event: MinionEvent): boolean {
  return (
    event.event_type === "heartbeat" ||
    event.event_type === "ping" ||
    event.event_type === "keepalive"
  );
}

/**
 * Extract messageID from event content if present.
 */
function getMessageID(event: MinionEvent): string | undefined {
  const content = event.content;
  // Check various locations where messageID might be
  if (typeof content.messageID === "string") return content.messageID;
  if (typeof content.message_id === "string") return content.message_id;
  // Check nested in properties
  const properties = content.properties as Record<string, unknown> | undefined;
  if (properties) {
    const part = properties.part as Record<string, unknown> | undefined;
    if (part && typeof part.messageID === "string") return part.messageID;
    if (typeof properties.messageID === "string") return properties.messageID;
  }
  // Check nested in part directly
  const part = content.part as Record<string, unknown> | undefined;
  if (part && typeof part.messageID === "string") return part.messageID;
  return undefined;
}

/**
 * Extract part type from event content.
 */
function getPartType(event: MinionEvent): string | undefined {
  const content = event.content;
  if (typeof content.type === "string") return content.type;
  const properties = content.properties as Record<string, unknown> | undefined;
  if (properties) {
    const part = properties.part as Record<string, unknown> | undefined;
    if (part && typeof part.type === "string") return part.type;
  }
  const part = content.part as Record<string, unknown> | undefined;
  if (part && typeof part.type === "string") return part.type;
  return undefined;
}

/**
 * Extract part ID from event content.
 */
function getPartID(event: MinionEvent): string | undefined {
  const content = event.content;
  if (typeof content.id === "string") return content.id;
  const properties = content.properties as Record<string, unknown> | undefined;
  if (properties) {
    const part = properties.part as Record<string, unknown> | undefined;
    if (part && typeof part.id === "string") return part.id;
    if (typeof properties.id === "string") return properties.id;
  }
  const part = content.part as Record<string, unknown> | undefined;
  if (part && typeof part.id === "string") return part.id;
  return undefined;
}

/**
 * Extract session ID from event content.
 * Events belong to a session, and subtask events have a different sessionID
 * than the parent session.
 */
export function getSessionID(event: MinionEvent): string | undefined {
  const content = event.content;
  // Check at root level
  if (typeof content.sessionID === "string") return content.sessionID;
  if (typeof content.session_id === "string") return content.session_id;
  // Check nested in properties
  const properties = content.properties as Record<string, unknown> | undefined;
  if (properties) {
    const part = properties.part as Record<string, unknown> | undefined;
    if (part && typeof part.sessionID === "string") return part.sessionID;
    if (typeof properties.sessionID === "string") return properties.sessionID;
  }
  // Check nested in part directly
  const part = content.part as Record<string, unknown> | undefined;
  if (part && typeof part.sessionID === "string") return part.sessionID;
  return undefined;
}

/**
 * Information about a subtask extracted from a task tool call.
 * The callID of the task tool becomes the session ID for the subtask.
 */
export interface SubtaskInfo {
  /** Session ID of the subtask (matches the callID of the task tool) */
  sessionID: string;
  /** Human-readable description from the task prompt */
  description: string;
  /** Agent type (e.g., "task", "explore", "code-reviewer") */
  agent?: string;
}

/**
 * Extract subtask info from a task tool part.
 * Task tool calls spawn subagents whose session ID matches the tool's callID.
 */
function extractSubtaskInfo(event: MinionEvent): SubtaskInfo | undefined {
  const content = event.content;
  const properties = content.properties as Record<string, unknown> | undefined;
  const part = (properties?.part || content.part || content) as Record<
    string,
    unknown
  >;

  // Check if this is a task tool call
  const partType = part.type as string | undefined;
  const tool = part.tool as string | undefined;
  
  if (partType !== "tool" || (tool !== "task" && tool !== "agent")) {
    return undefined;
  }

  // The callID becomes the subtask session ID
  const callID = (part.callID || part.call_id || part.id) as string | undefined;
  if (!callID) return undefined;

  // Extract description from the input
  const state = (part.state || part) as Record<string, unknown>;
  const input = state.input as Record<string, unknown> | undefined;
  const description = (input?.description || input?.prompt || "Subtask") as string;
  const agent = (input?.subagent_type || input?.agent || input?.type) as string | undefined;

  return {
    sessionID: callID,
    description: typeof description === "string" ? description : String(description),
    agent,
  };
}

/**
 * Determines if an event is a system-level event (no messageID or system part type).
 */
function isSystemEvent(event: MinionEvent): boolean {
  // Session-level events are always system events
  if (
    event.event_type === "session.error" ||
    event.event_type === "session.status" ||
    event.event_type === "error"
  ) {
    return true;
  }

  // Check part type for agent/retry which are system messages
  const partType = getPartType(event);
  if (partType && SYSTEM_PART_TYPES.has(partType)) {
    return true;
  }

  // Events without messageID are system events
  return !getMessageID(event);
}

/**
 * Should this event be skipped entirely (not rendered)?
 */
function shouldSkipEvent(event: MinionEvent): boolean {
  if (isHeartbeatEvent(event)) return true;

  const partType = getPartType(event);
  if (partType && SKIP_PART_TYPES.has(partType)) return true;

  const content = event.content;

  // Skip bare file change events: {file: string} or {event: string, file: string}
  // These are noise from file watcher notifications
  if (
    typeof content.file === "string" &&
    Object.keys(content).length <= 2
  ) {
    return true;
  }

  // Skip info events: {info: {...tokens, cost...}}
  // These are usage/cost metadata, not displayable content
  if (
    content.info !== null &&
    typeof content.info === "object" &&
    !Array.isArray(content.info)
  ) {
    return true;
  }

  // Skip diff events: {diff: [...], sessionID: "..."}
  // These are internal diff payloads, not displayable content
  if (Array.isArray(content.diff)) {
    return true;
  }

  return false;
}

/**
 * Convert event to SystemMessage.
 */
function eventToSystemMessage(event: MinionEvent): SystemMessage {
  const partType = getPartType(event);
  let type = event.event_type;
  let content: string | Record<string, unknown> = event.content;

  // Determine type and content based on event
  if (partType === "agent") {
    type = "agent";
    const properties = event.content.properties as
      | Record<string, unknown>
      | undefined;
    const part = (properties?.part || event.content.part) as
      | Record<string, unknown>
      | undefined;
    if (part && typeof part.agent === "string") {
      content = part.agent;
    }
  } else if (partType === "retry") {
    type = "retry";
    const properties = event.content.properties as
      | Record<string, unknown>
      | undefined;
    const part = (properties?.part || event.content.part) as
      | Record<string, unknown>
      | undefined;
    if (part) {
      content = part;
    }
  } else if (event.event_type === "session.error" || event.event_type === "error") {
    type = "session.error";
    content =
      (event.content.error as string) ||
      (event.content.message as string) ||
      event.content;
  } else {
    // Unknown event type - log warning for debugging
    console.warn(
      `[event-aggregation] Unknown event type: ${event.event_type}`,
      { eventId: event.id, partType }
    );
  }

  return {
    id: event.id,
    timestamp: event.timestamp,
    type,
    content,
  };
}

/**
 * Extract ToolCall from a tool part.
 */
function extractToolCall(
  event: MinionEvent,
  partID: string
): ToolCall | undefined {
  const content = event.content;
  const properties = content.properties as Record<string, unknown> | undefined;
  const part = (properties?.part || content.part || content) as Record<
    string,
    unknown
  >;

  if (part.type !== "tool") return undefined;

  const tool = (part.tool as string) || "unknown";
  const state = (part.state || part) as Record<string, unknown>;
  const status = (state.status as ToolCallStatus) || "pending";
  const input = (state.input as Record<string, unknown>) || {};
  const output = state.output as string | undefined;
  const error = state.error as string | undefined;
  const title = state.title as string | undefined;

  return {
    id: partID,
    tool,
    status,
    title,
    summary: "", // Will be set by getToolSummary later
    input,
    output,
    error,
  };
}

/**
 * Extract text content from a text or reasoning part.
 * Returns the partID along with the content for delta tracking.
 */
function extractTextContent(
  event: MinionEvent
): { type: "text" | "reasoning"; content: string; partID: string; isDelta: boolean } | undefined {
  const content = event.content;
  const properties = content.properties as Record<string, unknown> | undefined;
  const part = (properties?.part || content.part || content) as Record<
    string,
    unknown
  >;

  const partType = part.type as string | undefined;
  if (partType !== "text" && partType !== "reasoning") return undefined;

  // Get partID for delta tracking
  const partID = getPartID(event);
  if (!partID) return undefined;

  // Check if this is a delta event (appends to existing text)
  const isDelta = properties?.delta === true || content.delta === true;

  const text = (part.text as string) || "";
  return { type: partType, content: text, partID, isDelta };
}

/**
 * Aggregate raw MinionEvents into structured ChatMessages and SystemMessages.
 *
 * This function:
 * 1. Deduplicates events by ID
 * 2. Filters out heartbeat and internal events
 * 3. Sorts events by timestamp
 * 4. Groups events by messageID into ChatMessages
 * 5. Extracts system events (no messageID) as SystemMessages
 * 6. Handles message.updated events by updating metadata
 * 7. Accumulates delta events by appending to existing text buffers
 * 8. Recursively aggregates subtask events into nested ChatMessage arrays
 *
 * @param events - Raw events to aggregate
 * @param deltaState - Persistent state for delta accumulation (pass same instance across calls)
 * @param rootSessionID - Optional root session ID; if not provided, detected from events
 */
export function aggregateEvents(
  events: MinionEvent[],
  deltaState?: DeltaState,
  rootSessionID?: string
): AggregationResult {
  // Use provided state or create fresh one
  const state = deltaState || createDeltaState();

  // Deduplicate events by ID
  const eventMap = new Map<string, MinionEvent>();
  for (const event of events) {
    // Later events with same ID replace earlier ones (for updates)
    eventMap.set(event.id, event);
  }

  // Sort by timestamp
  const sortedEvents = Array.from(eventMap.values()).sort(
    (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );

  // If no root session ID provided, detect it from the most common session ID
  // or the first event's session ID
  let detectedRootSessionID = rootSessionID;
  if (!detectedRootSessionID) {
    const sessionCounts = new Map<string, number>();
    for (const event of sortedEvents) {
      const sid = getSessionID(event);
      if (sid) {
        sessionCounts.set(sid, (sessionCounts.get(sid) || 0) + 1);
      }
    }
    // Use the most common session ID as root
    let maxCount = 0;
    for (const [sid, count] of Array.from(sessionCounts.entries())) {
      if (count > maxCount) {
        maxCount = count;
        detectedRootSessionID = sid;
      }
    }
  }

  // Collect subtask session IDs from task tool calls
  // Maps subtask sessionID -> SubtaskInfo
  const subtaskInfoMap = new Map<string, SubtaskInfo>();
  for (const event of sortedEvents) {
    const subtaskInfo = extractSubtaskInfo(event);
    if (subtaskInfo) {
      subtaskInfoMap.set(subtaskInfo.sessionID, subtaskInfo);
    }
  }

  // Separate events into root session events vs subtask events
  const rootEvents: MinionEvent[] = [];
  const subtaskEventsMap = new Map<string, MinionEvent[]>();

  for (const event of sortedEvents) {
    const eventSessionID = getSessionID(event);
    
    // If no session ID, treat as root event
    if (!eventSessionID) {
      rootEvents.push(event);
      continue;
    }

    // Check if this belongs to a known subtask
    if (subtaskInfoMap.has(eventSessionID)) {
      const existing = subtaskEventsMap.get(eventSessionID) || [];
      existing.push(event);
      subtaskEventsMap.set(eventSessionID, existing);
    } else if (!detectedRootSessionID || eventSessionID === detectedRootSessionID) {
      // Belongs to root session
      rootEvents.push(event);
    } else {
      // Unknown session ID - might be a subtask we don't have info for
      // Treat as subtask if we have any events for it
      const existing = subtaskEventsMap.get(eventSessionID) || [];
      existing.push(event);
      subtaskEventsMap.set(eventSessionID, existing);
    }
  }

  // Process root events to build messages
  const systemMessages: SystemMessage[] = [];
  const chatEventsByMessage = new Map<
    string,
    { events: MinionEvent[]; earliestTimestamp: string }
  >();

  for (const event of rootEvents) {
    // Skip heartbeats and internal events
    if (shouldSkipEvent(event)) continue;

    // Handle system events
    if (isSystemEvent(event)) {
      systemMessages.push(eventToSystemMessage(event));
      continue;
    }

    // Group by messageID for chat messages
    const messageID = getMessageID(event);
    if (!messageID) continue; // Shouldn't happen after isSystemEvent check

    const existing = chatEventsByMessage.get(messageID);
    if (existing) {
      existing.events.push(event);
      // Track earliest timestamp
      if (new Date(event.timestamp) < new Date(existing.earliestTimestamp)) {
        existing.earliestTimestamp = event.timestamp;
      }
    } else {
      chatEventsByMessage.set(messageID, {
        events: [event],
        earliestTimestamp: event.timestamp,
      });
    }
  }

  // Build ChatMessages from grouped events
  const messages: ChatMessage[] = [];
  const messageEntries = Array.from(chatEventsByMessage.entries());

  // Sort message groups by earliest timestamp
  messageEntries.sort(
    (a, b) =>
      new Date(a[1].earliestTimestamp).getTime() -
      new Date(b[1].earliestTimestamp).getTime()
  );

  for (const [messageID, { events: messageEvents, earliestTimestamp }] of messageEntries) {
    const tools: ToolCall[] = [];
    const toolMap = new Map<string, ToolCall>();
    const subtasks: SubtaskThread[] = [];
    let isStreaming = false;

    // Track text and reasoning parts by their partID
    // We'll use the delta map to accumulate content
    const textPartIDs: string[] = [];
    const reasoningPartIDs: string[] = [];

    // Sort events within message by timestamp
    const sortedMessageEvents = [...messageEvents].sort(
      (a, b) =>
        new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
    );

    for (const event of sortedMessageEvents) {
      const partID = getPartID(event);
      const partType = getPartType(event);

      // Handle message.updated events (metadata updates)
      if (event.event_type === "message.updated") {
        // These update message metadata but don't change content
        // We primarily use them to check if message is still streaming
        const status = event.content.status as string | undefined;
        if (status === "streaming" || status === "pending") {
          isStreaming = true;
        }
        continue;
      }

      // Check for subtask info from task tool calls
      const subtaskInfo = extractSubtaskInfo(event);
      if (subtaskInfo) {
        // Get events for this subtask and recursively aggregate
        const subtaskEvents = subtaskEventsMap.get(subtaskInfo.sessionID) || [];
        if (subtaskEvents.length > 0) {
          // Create a separate delta state for the subtask to avoid pollution
          const subtaskDeltaState = createDeltaState();
          const subtaskResult = aggregateEvents(
            subtaskEvents,
            subtaskDeltaState,
            subtaskInfo.sessionID
          );
          subtasks.push({
            sessionID: subtaskInfo.sessionID,
            description: subtaskInfo.description,
            agent: subtaskInfo.agent,
            messages: subtaskResult.messages,
          });
        }
      }

      // Extract text/reasoning content with delta handling
      const textContent = extractTextContent(event);
      if (textContent) {
        const { type, content, partID: textPartID, isDelta } = textContent;

        if (isDelta) {
          // Delta event: only append if we haven't processed this event before
          if (!state.processedEventIds.has(event.id)) {
            const existing = state.textByPart.get(textPartID) || "";
            state.textByPart.set(textPartID, existing + content);
            state.processedEventIds.add(event.id);
          }
        } else {
          // Full replacement: set content directly (always process)
          state.textByPart.set(textPartID, content);
        }

        // Track which parts belong to text vs reasoning
        if (type === "reasoning") {
          if (!reasoningPartIDs.includes(textPartID)) {
            reasoningPartIDs.push(textPartID);
          }
        } else {
          if (!textPartIDs.includes(textPartID)) {
            textPartIDs.push(textPartID);
          }
        }
        continue;
      }

      // Extract tool calls
      if (partType === "tool" && partID) {
        const toolCall = extractToolCall(event, partID);
        if (toolCall) {
          // Update existing or add new
          const existing = toolMap.get(partID);
          if (existing) {
            // Update with newer status/output
            existing.status = toolCall.status;
            existing.output = toolCall.output ?? existing.output;
            existing.error = toolCall.error ?? existing.error;
            existing.title = toolCall.title ?? existing.title;
          } else {
            toolMap.set(partID, toolCall);
          }
        }
      }
    }

    // Build individual text parts for memoized rendering
    const textParts = textPartIDs
      .map((pid) => ({ id: pid, text: state.textByPart.get(pid) || "" }))
      .filter((part) => part.text.length > 0);

    // Build final text from accumulated parts (join with double newlines)
    const text = textParts.map((p) => p.text).join("\n\n");

    // Build final thinking from accumulated reasoning parts
    const thinking = reasoningPartIDs
      .map((pid) => state.textByPart.get(pid) || "")
      .filter((t) => t.length > 0)
      .join("\n\n");

    // Convert tool map to array (preserving insertion order)
    tools.push(...Array.from(toolMap.values()));

    // Determine if still streaming based on latest events
    // If we got recent updates without "completed" status, assume streaming
    if (messageEvents.length > 0) {
      const latestEvent = messageEvents[messageEvents.length - 1];
      const timeSinceLatest =
        Date.now() - new Date(latestEvent.timestamp).getTime();
      // If last event was recent (within 30 seconds), assume streaming
      if (timeSinceLatest < 30000) {
        isStreaming = true;
      }
    }

    messages.push({
      id: messageID,
      timestamp: earliestTimestamp,
      thinking: thinking || undefined,
      text,
      textParts,
      tools,
      subtasks,
      isStreaming,
    });
  }

  return { messages, systemMessages };
}

/**
 * Create a new delta state for tracking streaming text accumulation.
 * Pass the same instance to aggregateEvents across multiple calls
 * to properly accumulate delta events without double-counting.
 */
export function createDeltaState(): DeltaState {
  return {
    textByPart: new Map(),
    processedEventIds: new Set(),
  };
}
