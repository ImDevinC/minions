import { describe, it, expect } from "vitest";
import {
  aggregateEvents,
  createDeltaState,
  getSessionID,
  type DeltaState,
} from "./event-aggregation";
import type { MinionEvent } from "@/types/minion";

/**
 * Helper to create a text event with the given properties.
 */
function makeTextEvent(
  id: string,
  messageID: string,
  partID: string,
  text: string,
  opts: { delta?: boolean; timestamp?: string; sessionID?: string } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "part.updated",
    content: {
      messageID,
      sessionID: opts.sessionID,
      id: partID,
      type: "text",
      text,
      properties: opts.delta ? { delta: true } : {},
    },
  };
}

/**
 * Helper to create a reasoning event.
 */
function makeReasoningEvent(
  id: string,
  messageID: string,
  partID: string,
  text: string,
  opts: { delta?: boolean; timestamp?: string } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "part.updated",
    content: {
      messageID,
      id: partID,
      type: "reasoning",
      text,
      properties: opts.delta ? { delta: true } : {},
    },
  };
}

/**
 * Helper to create a task tool event that spawns a subtask.
 * The callID becomes the session ID for the subtask.
 */
function makeTaskToolEvent(
  id: string,
  messageID: string,
  partID: string,
  callID: string,
  opts: {
    timestamp?: string;
    sessionID?: string;
    description?: string;
    agent?: string;
    status?: string;
  } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "part.updated",
    content: {
      messageID,
      sessionID: opts.sessionID,
      id: partID,
      type: "tool",
      tool: "task",
      callID,
      state: {
        status: opts.status || "pending",
        input: {
          description: opts.description || "Execute a subtask",
          subagent_type: opts.agent || "general",
        },
      },
    },
  };
}

describe("event-aggregation", () => {
  describe("delta accumulation", () => {
    it("appends delta events to existing text buffer", () => {
      const deltaState = createDeltaState();

      // First delta event
      const event1 = makeTextEvent("e1", "msg1", "part1", "Hello", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });

      // Second delta event (same part)
      const event2 = makeTextEvent("e2", "msg1", "part1", " world", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });

      // Aggregate with same deltaState across calls
      aggregateEvents([event1], deltaState);
      const result = aggregateEvents([event1, event2], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Hello world");
    });

    it("replaces content for non-delta events", () => {
      const deltaState = createDeltaState();

      // First event sets text
      const event1 = makeTextEvent("e1", "msg1", "part1", "Hello", {
        delta: false,
        timestamp: "2024-01-01T00:00:00Z",
      });

      // Second event replaces (non-delta)
      const event2 = makeTextEvent("e2", "msg1", "part1", "Goodbye", {
        delta: false,
        timestamp: "2024-01-01T00:00:01Z",
      });

      const result = aggregateEvents([event1, event2], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Goodbye");
    });

    it("delta state persists across aggregation calls", () => {
      const deltaState = createDeltaState();

      // First call with first delta
      const event1 = makeTextEvent("e1", "msg1", "part1", "A", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });
      aggregateEvents([event1], deltaState);

      // Second call adds more delta events
      const event2 = makeTextEvent("e2", "msg1", "part1", "B", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });
      aggregateEvents([event1, event2], deltaState);

      // Third call adds even more
      const event3 = makeTextEvent("e3", "msg1", "part1", "C", {
        delta: true,
        timestamp: "2024-01-01T00:00:02Z",
      });
      const result = aggregateEvents([event1, event2, event3], deltaState);

      // Should accumulate all deltas without double-counting
      expect(result.messages[0].text).toBe("ABC");
    });

    it("concatenates multiple text parts with double newlines", () => {
      const deltaState = createDeltaState();

      // Two different parts in the same message
      const event1 = makeTextEvent("e1", "msg1", "part1", "First paragraph", {
        delta: false,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeTextEvent("e2", "msg1", "part2", "Second paragraph", {
        delta: false,
        timestamp: "2024-01-01T00:00:01Z",
      });

      const result = aggregateEvents([event1, event2], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe(
        "First paragraph\n\nSecond paragraph"
      );
    });

    it("handles mixed delta and replacement within same message", () => {
      const deltaState = createDeltaState();

      // Part 1: delta streaming
      const event1 = makeTextEvent("e1", "msg1", "part1", "Streaming ", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeTextEvent("e2", "msg1", "part1", "text", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });

      // Part 2: full replacement
      const event3 = makeTextEvent("e3", "msg1", "part2", "Fixed text", {
        delta: false,
        timestamp: "2024-01-01T00:00:02Z",
      });

      const result = aggregateEvents([event1, event2, event3], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Streaming text\n\nFixed text");
    });

    it("handles reasoning parts with delta accumulation", () => {
      const deltaState = createDeltaState();

      const event1 = makeReasoningEvent("e1", "msg1", "think1", "Let me ", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeReasoningEvent("e2", "msg1", "think1", "think...", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });

      const result = aggregateEvents([event1, event2], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].thinking).toBe("Let me think...");
    });

    it("uses fresh delta state when not provided", () => {
      // First aggregation without passing deltaState
      const event1 = makeTextEvent("e1", "msg1", "part1", "Hello", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });

      const result1 = aggregateEvents([event1]);
      expect(result1.messages[0].text).toBe("Hello");

      // Second aggregation - fresh state, so no accumulation from previous call
      const event2 = makeTextEvent("e2", "msg1", "part1", " world", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });

      // Without passing same deltaState, accumulation doesn't persist
      const result2 = aggregateEvents([event1, event2]);
      // Each call gets fresh state, so deltas accumulate within that call
      expect(result2.messages[0].text).toBe("Hello world");
    });
  });

  describe("full replacement behavior", () => {
    it("replaces text when delta is false", () => {
      const deltaState = createDeltaState();

      const event1 = makeTextEvent("e1", "msg1", "part1", "First", {
        delta: false,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeTextEvent("e2", "msg1", "part1", "Second", {
        delta: false,
        timestamp: "2024-01-01T00:00:01Z",
      });
      const event3 = makeTextEvent("e3", "msg1", "part1", "Third", {
        delta: false,
        timestamp: "2024-01-01T00:00:02Z",
      });

      const result = aggregateEvents([event1, event2, event3], deltaState);

      expect(result.messages[0].text).toBe("Third");
    });

    it("replaces accumulated delta with non-delta event", () => {
      const deltaState = createDeltaState();

      // Accumulate some deltas
      const event1 = makeTextEvent("e1", "msg1", "part1", "A", {
        delta: true,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeTextEvent("e2", "msg1", "part1", "B", {
        delta: true,
        timestamp: "2024-01-01T00:00:01Z",
      });

      aggregateEvents([event1, event2], deltaState);

      // Now a replacement event
      const event3 = makeTextEvent("e3", "msg1", "part1", "Replaced", {
        delta: false,
        timestamp: "2024-01-01T00:00:02Z",
      });

      const result = aggregateEvents([event1, event2, event3], deltaState);

      expect(result.messages[0].text).toBe("Replaced");
    });

    it("handles empty text content gracefully", () => {
      const deltaState = createDeltaState();

      const event = makeTextEvent("e1", "msg1", "part1", "", {
        delta: false,
        timestamp: "2024-01-01T00:00:00Z",
      });

      const result = aggregateEvents([event], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("");
    });

    it("populates textParts with part IDs for memoized rendering", () => {
      const deltaState = createDeltaState();

      const event1 = makeTextEvent("e1", "msg1", "part1", "First paragraph", {
        delta: false,
        timestamp: "2024-01-01T00:00:00Z",
      });
      const event2 = makeTextEvent("e2", "msg1", "part2", "Second paragraph", {
        delta: false,
        timestamp: "2024-01-01T00:00:01Z",
      });

      const result = aggregateEvents([event1, event2], deltaState);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].textParts).toHaveLength(2);
      expect(result.messages[0].textParts[0]).toEqual({ id: "part1", text: "First paragraph" });
      expect(result.messages[0].textParts[1]).toEqual({ id: "part2", text: "Second paragraph" });
    });
  });

  describe("subtask aggregation", () => {
    it("extracts session ID from events", () => {
      const event = makeTextEvent("e1", "msg1", "part1", "Hello", {
        sessionID: "session-123",
      });

      expect(getSessionID(event)).toBe("session-123");
    });

    it("groups subtask events by session ID", () => {
      const rootSessionID = "root-session";
      const subtaskSessionID = "subtask-session";

      // Parent message with a task tool call
      const taskToolEvent = makeTaskToolEvent(
        "e1",
        "parent-msg",
        "tool-part",
        subtaskSessionID,
        {
          timestamp: "2024-01-01T00:00:00Z",
          sessionID: rootSessionID,
          description: "Find config files",
          agent: "explore",
        }
      );

      // Root session text event
      const rootTextEvent = makeTextEvent(
        "e2",
        "parent-msg",
        "text-part",
        "Let me search...",
        {
          timestamp: "2024-01-01T00:00:01Z",
          sessionID: rootSessionID,
        }
      );

      // Subtask session events (different session ID)
      const subtaskTextEvent = makeTextEvent(
        "e3",
        "subtask-msg",
        "subtask-text",
        "Found 3 files",
        {
          timestamp: "2024-01-01T00:00:02Z",
          sessionID: subtaskSessionID,
        }
      );

      const result = aggregateEvents(
        [taskToolEvent, rootTextEvent, subtaskTextEvent],
        createDeltaState(),
        rootSessionID
      );

      // Should have 1 root message with subtask
      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Let me search...");
      expect(result.messages[0].subtasks).toHaveLength(1);

      // Subtask should have its own nested message
      const subtask = result.messages[0].subtasks[0];
      expect(subtask.sessionID).toBe(subtaskSessionID);
      expect(subtask.description).toBe("Find config files");
      expect(subtask.agent).toBe("explore");
      expect(subtask.messages).toHaveLength(1);
      expect(subtask.messages[0].text).toBe("Found 3 files");
    });

    it("recursively aggregates nested subtask messages", () => {
      const rootSessionID = "root-session";
      const subtaskSessionID = "subtask-session";

      // Task tool call in parent
      const taskToolEvent = makeTaskToolEvent(
        "e1",
        "parent-msg",
        "tool-part",
        subtaskSessionID,
        {
          timestamp: "2024-01-01T00:00:00Z",
          sessionID: rootSessionID,
          description: "Research task",
        }
      );

      // Multiple messages in subtask session
      const subtaskEvent1 = makeTextEvent(
        "e2",
        "subtask-msg-1",
        "part1",
        "First subtask message",
        {
          timestamp: "2024-01-01T00:00:01Z",
          sessionID: subtaskSessionID,
        }
      );

      const subtaskEvent2 = makeTextEvent(
        "e3",
        "subtask-msg-2",
        "part2",
        "Second subtask message",
        {
          timestamp: "2024-01-01T00:00:02Z",
          sessionID: subtaskSessionID,
        }
      );

      const result = aggregateEvents(
        [taskToolEvent, subtaskEvent1, subtaskEvent2],
        createDeltaState(),
        rootSessionID
      );

      expect(result.messages).toHaveLength(1);
      const subtask = result.messages[0].subtasks[0];
      expect(subtask.messages).toHaveLength(2);
      expect(subtask.messages[0].text).toBe("First subtask message");
      expect(subtask.messages[1].text).toBe("Second subtask message");
    });

    it("handles messages without subtasks", () => {
      const result = aggregateEvents([
        makeTextEvent("e1", "msg1", "part1", "Hello", {
          timestamp: "2024-01-01T00:00:00Z",
        }),
      ]);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].subtasks).toHaveLength(0);
    });

    it("separates events from multiple subtask sessions", () => {
      const rootSessionID = "root";
      const subtask1ID = "subtask-1";
      const subtask2ID = "subtask-2";

      // Two task tool calls
      const task1 = makeTaskToolEvent("e1", "msg", "tool1", subtask1ID, {
        timestamp: "2024-01-01T00:00:00Z",
        sessionID: rootSessionID,
        description: "Task 1",
      });

      const task2 = makeTaskToolEvent("e2", "msg", "tool2", subtask2ID, {
        timestamp: "2024-01-01T00:00:01Z",
        sessionID: rootSessionID,
        description: "Task 2",
      });

      // Events for each subtask
      const subtask1Event = makeTextEvent(
        "e3",
        "subtask1-msg",
        "part1",
        "From subtask 1",
        { timestamp: "2024-01-01T00:00:02Z", sessionID: subtask1ID }
      );

      const subtask2Event = makeTextEvent(
        "e4",
        "subtask2-msg",
        "part2",
        "From subtask 2",
        { timestamp: "2024-01-01T00:00:03Z", sessionID: subtask2ID }
      );

      const result = aggregateEvents(
        [task1, task2, subtask1Event, subtask2Event],
        createDeltaState(),
        rootSessionID
      );

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].subtasks).toHaveLength(2);

      const subtask1 = result.messages[0].subtasks.find(
        (s) => s.sessionID === subtask1ID
      );
      const subtask2 = result.messages[0].subtasks.find(
        (s) => s.sessionID === subtask2ID
      );

      expect(subtask1?.description).toBe("Task 1");
      expect(subtask1?.messages[0].text).toBe("From subtask 1");
      expect(subtask2?.description).toBe("Task 2");
      expect(subtask2?.messages[0].text).toBe("From subtask 2");
    });
  });
});
