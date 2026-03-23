import { describe, it, expect } from "vitest";
import { deriveSessionState } from "./chat-view";
import type { MinionEvent } from "@/types/minion";

/**
 * Helper to create a basic event with given properties.
 */
function makeEvent(
  id: string,
  eventType: string,
  content: Record<string, unknown> = {},
  timestamp?: string
): MinionEvent {
  return {
    id,
    timestamp: timestamp || new Date().toISOString(),
    event_type: eventType,
    content,
  };
}

describe("deriveSessionState", () => {
  describe("status bar visibility for pending minions", () => {
    it("returns null when events array is empty", () => {
      // Status bar should be hidden when no events available
      const result = deriveSessionState([]);
      expect(result).toBeNull();
    });

    it("returns idle when events exist but no activity indicators", () => {
      // Once events arrive, status bar should appear (at minimum showing idle)
      const event = makeEvent("e1", "part.updated", {
        messageID: "msg1",
        type: "text",
        text: "Hello",
      });

      const result = deriveSessionState([event]);
      expect(result).toBe("idle");
    });
  });

  describe("busy state detection", () => {
    it("detects retry events", () => {
      const event = makeEvent("e1", "retry", { type: "retry" });
      expect(deriveSessionState([event])).toBe("retry");
    });

    it("detects retry via content.type", () => {
      const event = makeEvent("e1", "part.updated", { type: "retry" });
      expect(deriveSessionState([event])).toBe("retry");
    });

    it("detects status.thinking = true", () => {
      const event = makeEvent("e1", "part.updated", {
        status: { thinking: true },
      });
      expect(deriveSessionState([event])).toBe("busy");
    });

    it("detects message.updated with streaming status", () => {
      const event = makeEvent("e1", "message.updated", {
        status: "streaming",
      });
      expect(deriveSessionState([event])).toBe("busy");
    });

    it("detects message.updated with pending status", () => {
      const event = makeEvent("e1", "message.updated", {
        status: "pending",
      });
      expect(deriveSessionState([event])).toBe("busy");
    });

    it("detects part.delta events", () => {
      const event = makeEvent("e1", "part.delta", {
        messageID: "msg1",
        delta: "some text",
      });
      expect(deriveSessionState([event])).toBe("busy");
    });

    it("detects text.delta events", () => {
      const event = makeEvent("e1", "text.delta", {
        messageID: "msg1",
        delta: "some text",
      });
      expect(deriveSessionState([event])).toBe("busy");
    });
  });

  describe("recent events priority", () => {
    it("prioritizes most recent event indicators", () => {
      // Earlier event is busy, but most recent (in check order) is retry
      const events = [
        makeEvent("e1", "part.delta", {}, "2024-01-01T00:00:00Z"),
        makeEvent("e2", "retry", { type: "retry" }, "2024-01-01T00:00:01Z"),
      ];
      expect(deriveSessionState(events)).toBe("retry");
    });

    it("only checks last 10 events", () => {
      // Create 15 events: first 5 are retries, last 10 are plain
      const events: MinionEvent[] = [];
      for (let i = 0; i < 5; i++) {
        events.push(
          makeEvent(`retry-${i}`, "retry", { type: "retry" }, `2024-01-01T00:00:0${i}Z`)
        );
      }
      for (let i = 5; i < 15; i++) {
        events.push(
          makeEvent(`plain-${i}`, "part.updated", { type: "text" }, `2024-01-01T00:00:${i}Z`)
        );
      }

      // The retry events are outside the last 10, so should be idle
      expect(deriveSessionState(events)).toBe("idle");
    });

    it("detects activity in recent 10 events", () => {
      // Create 10 plain events + 1 recent retry
      const events: MinionEvent[] = [];
      for (let i = 0; i < 10; i++) {
        events.push(
          makeEvent(`plain-${i}`, "part.updated", { type: "text" }, `2024-01-01T00:00:0${i}Z`)
        );
      }
      events.push(
        makeEvent("retry", "retry", { type: "retry" }, "2024-01-01T00:00:10Z")
      );

      expect(deriveSessionState(events)).toBe("retry");
    });
  });

  describe("idle fallback", () => {
    it("returns idle when no activity indicators found in recent events", () => {
      const event = makeEvent("e1", "part.updated", {
        messageID: "msg1",
        type: "tool",
        tool: "bash",
        state: { status: "completed" },
      });
      expect(deriveSessionState([event])).toBe("idle");
    });

    it("returns idle for status.thinking = false", () => {
      const event = makeEvent("e1", "part.updated", {
        status: { thinking: false },
      });
      expect(deriveSessionState([event])).toBe("idle");
    });

    it("returns idle for message.updated with completed status", () => {
      const event = makeEvent("e1", "message.updated", {
        status: "completed",
      });
      expect(deriveSessionState([event])).toBe("idle");
    });
  });
});
