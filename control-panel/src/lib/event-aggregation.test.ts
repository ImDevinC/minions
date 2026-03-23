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

/**
 * Helper to create a message.part.updated event (announces part type).
 */
function makePartUpdatedEvent(
  id: string,
  messageID: string,
  partID: string,
  partType: "text" | "reasoning",
  text: string,
  opts: { timestamp?: string; sessionID?: string } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "message.part.updated",
    content: {
      messageID,
      sessionID: opts.sessionID,
      properties: {
        part: {
          id: partID,
          type: partType,
          text,
        },
      },
    },
  };
}

/**
 * Helper to create a message.part.delta event (streaming chunk).
 * These have flat structure: content.partID, content.delta, content.field
 */
function makePartDeltaEvent(
  id: string,
  messageID: string,
  partID: string,
  delta: string,
  opts: { timestamp?: string; sessionID?: string; field?: string } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "message.part.delta",
    content: {
      messageID,
      sessionID: opts.sessionID,
      partID,
      delta,
      field: opts.field || "text",
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

  describe("message.part.delta event handling", () => {
    it("accumulates text delta events from flat event structure", () => {
      const deltaState = createDeltaState();

      // First, a part.updated event announces the part type
      const partUpdated = makePartUpdatedEvent(
        "e1",
        "msg1",
        "part1",
        "text",
        "",
        { timestamp: "2024-01-01T00:00:00Z" }
      );

      // Then delta events stream in with flat structure
      const delta1 = makePartDeltaEvent("e2", "msg1", "part1", "Hello", {
        timestamp: "2024-01-01T00:00:01Z",
      });
      const delta2 = makePartDeltaEvent("e3", "msg1", "part1", " world", {
        timestamp: "2024-01-01T00:00:02Z",
      });

      const result = aggregateEvents(
        [partUpdated, delta1, delta2],
        deltaState
      );

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Hello world");
    });

    it("routes reasoning delta events to thinking field", () => {
      const deltaState = createDeltaState();

      // Part.updated event announces this is a reasoning part
      const partUpdated = makePartUpdatedEvent(
        "e1",
        "msg1",
        "think1",
        "reasoning",
        "",
        { timestamp: "2024-01-01T00:00:00Z" }
      );

      // Delta events stream reasoning content
      const delta1 = makePartDeltaEvent("e2", "msg1", "think1", "Let me ", {
        timestamp: "2024-01-01T00:00:01Z",
      });
      const delta2 = makePartDeltaEvent("e3", "msg1", "think1", "think...", {
        timestamp: "2024-01-01T00:00:02Z",
      });

      const result = aggregateEvents(
        [partUpdated, delta1, delta2],
        deltaState
      );

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].thinking).toBe("Let me think...");
      expect(result.messages[0].text).toBe(""); // No text content
    });

    it("defaults to text when delta arrives before part.updated (race condition)", () => {
      const deltaState = createDeltaState();

      // Delta arrives before we know the part type
      const delta = makePartDeltaEvent("e1", "msg1", "part1", "Early delta", {
        timestamp: "2024-01-01T00:00:00Z",
      });

      const result = aggregateEvents([delta], deltaState);

      expect(result.messages).toHaveLength(1);
      // Defaults to text when type is unknown
      expect(result.messages[0].text).toBe("Early delta");
      expect(result.messages[0].thinking).toBeUndefined();
    });

    it("handles mixed text and reasoning delta streams", () => {
      const deltaState = createDeltaState();

      // Announce both part types
      const textPart = makePartUpdatedEvent(
        "e1",
        "msg1",
        "text1",
        "text",
        "",
        { timestamp: "2024-01-01T00:00:00Z" }
      );
      const reasoningPart = makePartUpdatedEvent(
        "e2",
        "msg1",
        "think1",
        "reasoning",
        "",
        { timestamp: "2024-01-01T00:00:01Z" }
      );

      // Interleaved deltas for both
      const textDelta = makePartDeltaEvent("e3", "msg1", "text1", "Answer", {
        timestamp: "2024-01-01T00:00:02Z",
      });
      const thinkDelta = makePartDeltaEvent(
        "e4",
        "msg1",
        "think1",
        "Reasoning",
        { timestamp: "2024-01-01T00:00:03Z" }
      );

      const result = aggregateEvents(
        [textPart, reasoningPart, textDelta, thinkDelta],
        deltaState
      );

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Answer");
      expect(result.messages[0].thinking).toBe("Reasoning");
    });

    it("deduplicates delta events by event ID", () => {
      const deltaState = createDeltaState();

      const partUpdated = makePartUpdatedEvent(
        "e1",
        "msg1",
        "part1",
        "text",
        "",
        { timestamp: "2024-01-01T00:00:00Z" }
      );

      const delta = makePartDeltaEvent("e2", "msg1", "part1", "Hello", {
        timestamp: "2024-01-01T00:00:01Z",
      });

      // Same event passed twice (simulating re-aggregation)
      aggregateEvents([partUpdated, delta], deltaState);
      const result = aggregateEvents([partUpdated, delta, delta], deltaState);

      // Should only count once
      expect(result.messages[0].text).toBe("Hello");
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

  describe("event filtering - skip logic", () => {
    it("skips heartbeat events", () => {
      const heartbeat: MinionEvent = {
        id: "hb1",
        timestamp: new Date().toISOString(),
        event_type: "heartbeat",
        content: {},
      };
      const ping: MinionEvent = {
        id: "ping1",
        timestamp: new Date().toISOString(),
        event_type: "ping",
        content: {},
      };
      const keepalive: MinionEvent = {
        id: "ka1",
        timestamp: new Date().toISOString(),
        event_type: "keepalive",
        content: {},
      };

      const result = aggregateEvents([heartbeat, ping, keepalive]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips step-start events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          type: "step-start",
          id: "step1",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips step-finish events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          type: "step-finish",
          id: "step1",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips snapshot events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          type: "snapshot",
          data: { some: "snapshot" },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips patch events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          type: "patch",
          operations: [],
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips compaction events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          type: "compaction",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips bare file events with 1 key", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          file: "/path/to/file.go",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips bare file events with 2 keys", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          event: "change",
          file: "/path/to/file.go",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips info object events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          info: { tokens: 100, cost: 0.01 },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips diff array events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          diff: [{ op: "add", path: "/foo" }],
          sessionID: "session-123",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips bare status events without messageID", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          sessionID: "session-123",
          status: { thinking: true },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });

    it("skips summary events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          sessionID: "session-123",
          summary: { tokens: 100, cost: 0.01 },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(0);
    });
  });

  describe("legitimate events render correctly", () => {
    it("renders text events", () => {
      const event = makeTextEvent("e1", "msg1", "part1", "Hello world", {
        timestamp: "2024-01-01T00:00:00Z",
      });

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Hello world");
      expect(result.messages[0].id).toBe("msg1");
    });

    it("renders tool call events", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          id: "tool1",
          type: "tool",
          tool: "bash",
          state: {
            status: "completed",
            input: { command: "ls -la" },
            output: "file1.txt\nfile2.txt",
          },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].tools).toHaveLength(1);
      expect(result.messages[0].tools[0].tool).toBe("bash");
      expect(result.messages[0].tools[0].status).toBe("completed");
      expect(result.messages[0].tools[0].input).toEqual({ command: "ls -la" });
      expect(result.messages[0].tools[0].output).toBe("file1.txt\nfile2.txt");
    });

    it("renders reasoning/thinking events", () => {
      const event = makeReasoningEvent(
        "e1",
        "msg1",
        "think1",
        "Let me think about this...",
        { timestamp: "2024-01-01T00:00:00Z" }
      );

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].thinking).toBe("Let me think about this...");
    });

    it("renders agent events as system messages", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          type: "agent",
          properties: {
            part: {
              type: "agent",
              agent: "code-reviewer",
            },
          },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(1);
      expect(result.systemMessages[0].type).toBe("agent");
      expect(result.systemMessages[0].content).toBe("code-reviewer");
    });

    it("renders retry events as system messages", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          type: "retry",
          properties: {
            part: {
              type: "retry",
              attempt: 2,
              reason: "rate limit",
            },
          },
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(1);
      expect(result.systemMessages[0].type).toBe("retry");
      expect(result.systemMessages[0].content).toEqual({
        type: "retry",
        attempt: 2,
        reason: "rate limit",
      });
    });

    it("renders session.error events as system messages", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "session.error",
        content: {
          error: "Connection failed",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(1);
      expect(result.systemMessages[0].type).toBe("session.error");
      expect(result.systemMessages[0].content).toBe("Connection failed");
    });

    it("renders error events as system messages", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "error",
        content: {
          message: "Something went wrong",
        },
      };

      const result = aggregateEvents([event]);

      expect(result.messages).toHaveLength(0);
      expect(result.systemMessages).toHaveLength(1);
      expect(result.systemMessages[0].type).toBe("session.error");
      expect(result.systemMessages[0].content).toBe("Something went wrong");
    });

    it("does NOT skip status events with messageID (message-scoped status)", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          sessionID: "session-123",
          messageID: "msg1",
          status: { thinking: true },
          type: "text",
          id: "part1",
          text: "Processing...",
        },
      };

      const result = aggregateEvents([event]);

      // Should render because it has messageID
      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("Processing...");
    });

    it("does NOT skip file events with more than 2 keys", () => {
      const event: MinionEvent = {
        id: "e1",
        timestamp: new Date().toISOString(),
        event_type: "part.updated",
        content: {
          messageID: "msg1",
          id: "part1",
          type: "text",
          file: "/path/to/file.go",
          text: "File content here",
        },
      };

      const result = aggregateEvents([event]);

      // Should render because it has more than 2 keys
      expect(result.messages).toHaveLength(1);
      expect(result.messages[0].text).toBe("File content here");
    });
  });
});
