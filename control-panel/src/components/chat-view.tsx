"use client";

import { useRef, useState, useCallback, useMemo, useEffect } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { MinionEvent, ChatMessage, SystemMessage } from "@/types/minion";
import {
  aggregateEvents,
  createDeltaState,
  type DeltaState,
} from "@/lib/event-aggregation";
import { ChatMessageRow } from "./chat-message-row";
import { SystemMessageRow } from "./system-message-row";

/**
 * Union type for virtualized list items.
 * We need to render both ChatMessages and SystemMessages in the same list.
 */
type RenderItem =
  | { type: "chat"; message: ChatMessage }
  | { type: "system"; message: SystemMessage };

interface ChatViewProps {
  /** Raw events from the SSE stream */
  events: MinionEvent[];
}

/**
 * ChatView renders a virtualized list of aggregated chat messages and system events.
 *
 * Features:
 * - Takes raw MinionEvent[] and aggregates into ChatMessage/SystemMessage
 * - Uses @tanstack/react-virtual for efficient rendering of large message lists
 * - Maintains persistent delta state for streaming text accumulation
 * - ~20 items overscan above/below viewport for smooth scrolling
 */
export function ChatView({ events }: ChatViewProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  
  // Track which tool card is expanded (for accordion behavior)
  // Format: "messageId:toolId" to uniquely identify across messages
  const [expandedToolId, setExpandedToolId] = useState<string | null>(null);

  // Persistent delta state for streaming text accumulation
  // This ref persists across renders so delta events properly accumulate
  const deltaStateRef = useRef<DeltaState>(createDeltaState());

  // Aggregate events into chat messages and system messages
  const { messages, systemMessages } = useMemo(() => {
    return aggregateEvents(events, deltaStateRef.current);
  }, [events]);

  // Combine and sort all items for rendering in chronological order
  const renderItems: RenderItem[] = useMemo(() => {
    const items: RenderItem[] = [];

    // Add all chat messages
    for (const msg of messages) {
      items.push({ type: "chat", message: msg });
    }

    // Add all system messages
    for (const msg of systemMessages) {
      items.push({ type: "system", message: msg });
    }

    // Sort by timestamp
    items.sort((a, b) => {
      const timeA = new Date(a.message.timestamp).getTime();
      const timeB = new Date(b.message.timestamp).getTime();
      return timeA - timeB;
    });

    return items;
  }, [messages, systemMessages]);

  // Virtual list setup with ~20 items overscan
  const virtualizer = useVirtualizer({
    count: renderItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      // Estimate row height based on type
      const item = renderItems[index];
      if (item.type === "system") {
        return 32; // System messages are compact
      }
      // Chat messages vary; estimate based on content length
      const msg = item.message;
      const textLength = msg.text.length;
      const hasTools = msg.tools.length > 0;
      const hasSubtasks = msg.subtasks.length > 0;
      // Base height + extra for longer text + tools/subtasks
      return 80 + Math.min(textLength / 10, 200) + (hasTools ? 24 : 0) + (hasSubtasks ? 24 : 0);
    },
    overscan: 20, // Render ~20 extra items above/below viewport
  });

  // Auto-scroll to bottom when new items arrive
  useEffect(() => {
    if (autoScroll && renderItems.length > 0) {
      virtualizer.scrollToIndex(renderItems.length - 1, { align: "end" });
    }
  }, [renderItems.length, autoScroll, virtualizer]);

  // Detect manual scroll to disable auto-scroll
  const handleScroll = useCallback(() => {
    if (!parentRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = parentRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 100;
    setAutoScroll(isAtBottom);
  }, []);

  // Empty state
  if (renderItems.length === 0) {
    return (
      <div className="flex items-center justify-center h-64 text-gray-500">
        No events yet
      </div>
    );
  }

  return (
    <div className="relative">
      {/* Jump to latest button */}
      {!autoScroll && (
        <button
          onClick={() => {
            setAutoScroll(true);
            virtualizer.scrollToIndex(renderItems.length - 1, { align: "end" });
          }}
          className="absolute bottom-4 right-4 z-10 bg-blue-600 hover:bg-blue-500 text-white text-xs px-3 py-1.5 rounded-full shadow-lg transition-colors"
        >
          ↓ Jump to latest
        </button>
      )}

      <div
        ref={parentRef}
        onScroll={handleScroll}
        className="h-[500px] overflow-auto bg-gray-900 rounded-lg border border-gray-700"
      >
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: "100%",
            position: "relative",
          }}
        >
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const item = renderItems[virtualRow.index];
            return (
              <div
                key={virtualRow.key}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${virtualRow.start}px)`,
                }}
                data-index={virtualRow.index}
                ref={virtualizer.measureElement}
              >
                {item.type === "chat" ? (
                  <ChatMessageRow
                    message={item.message}
                    expandedToolId={
                      expandedToolId?.startsWith(`${item.message.id}:`)
                        ? expandedToolId.slice(item.message.id.length + 1)
                        : null
                    }
                    onToolToggle={(toolId) => {
                      setExpandedToolId(
                        toolId ? `${item.message.id}:${toolId}` : null
                      );
                    }}
                  />
                ) : (
                  <SystemMessageRow message={item.message} />
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
