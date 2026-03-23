import { describe, it, expect } from "vitest";
import {
  aggregateEvents,
  createDeltaState,
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
  opts: { delta?: boolean; timestamp?: string } = {}
): MinionEvent {
  return {
    id,
    timestamp: opts.timestamp || new Date().toISOString(),
    event_type: "part.updated",
    content: {
      messageID,
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
  });
});
