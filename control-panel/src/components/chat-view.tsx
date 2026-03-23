"use client";

import { useRef, useState, useCallback, useMemo, useEffect } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { MinionEvent, ChatMessage, SystemMessage, MinionStatus } from "@/types/minion";
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
  /** Current minion status for empty state logic */
  status?: MinionStatus;
  /** Whether SSE connection is established */
  isConnected?: boolean;
  /** Whether component is in initial loading state (server render) */
  isLoading?: boolean;
  /** Whether SSE just reconnected and is fetching missed events */
  isCatchingUp?: boolean;
}

/**
 * ChatView renders a virtualized list of aggregated chat messages and system events.
 *
 * Features:
 * - Takes raw MinionEvent[] and aggregates into ChatMessage/SystemMessage
 * - Uses @tanstack/react-virtual for efficient rendering of large message lists
 * - Maintains persistent delta state for streaming text accumulation
 * - ~20 items overscan above/below viewport for smooth scrolling
 * - Shows appropriate loading/empty states based on minion status
 */
export function ChatView({ 
  events, 
  status, 
  isConnected,
  isLoading = false,
  isCatchingUp = false,
}: ChatViewProps) {
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

  // Helper: check if minion is in a terminal state
  const isTerminalStatus = status === "completed" || status === "failed" || status === "terminated";

  // Loading state: show skeleton loaders during initial page load
  if (isLoading) {
    return (
      <div className="h-[500px] bg-gray-900 rounded-lg border border-gray-700 p-4 space-y-4">
        {/* Skeleton message rows */}
        {[1, 2, 3].map((i) => (
          <div key={i} className="animate-pulse space-y-2">
            <div className="flex items-center gap-2">
              <div className="h-3 w-16 bg-gray-700 rounded" />
              <div className="h-5 w-5 bg-gray-700 rounded" />
            </div>
            <div className="space-y-2 pl-6">
              <div className="h-4 bg-gray-700 rounded w-3/4" />
              <div className="h-4 bg-gray-700 rounded w-1/2" />
            </div>
          </div>
        ))}
      </div>
    );
  }

  // Empty state handling based on minion status
  if (renderItems.length === 0) {
    // Running/pending minion with no events: agent is starting up
    if (status === "running" || status === "pending") {
      return (
        <div className="flex flex-col items-center justify-center h-64 text-gray-400 gap-2">
          <div className="flex items-center gap-2">
            <span className="relative flex h-3 w-3">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-3 w-3 bg-blue-500" />
            </span>
            <span>Waiting for agent to start...</span>
          </div>
        </div>
      );
    }

    // Completed minion with zero events: task produced no output
    if (isTerminalStatus && status === "completed") {
      return (
        <div className="flex items-center justify-center h-64 text-gray-500">
          No output produced
        </div>
      );
    }

    // Default empty state (failed/terminated with no events, or unknown status)
    return (
      <div className="flex items-center justify-center h-64 text-gray-500">
        No events yet
      </div>
    );
  }

  return (
    <div className="relative">
      {/* Catching up indicator - shown after SSE reconnection */}
      {isCatchingUp && (
        <div className="absolute top-2 left-1/2 -translate-x-1/2 z-20 bg-yellow-500/90 text-yellow-900 text-xs px-3 py-1 rounded-full shadow-lg flex items-center gap-2">
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-yellow-900 opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-yellow-900" />
          </span>
          Catching up...
        </div>
      )}

      {/* Jump to latest button - repositions on smaller screens */}
      {!autoScroll && (
        <button
          onClick={() => {
            setAutoScroll(true);
            virtualizer.scrollToIndex(renderItems.length - 1, { align: "end" });
          }}
          className="absolute bottom-3 right-2 md:bottom-4 md:right-4 z-10 bg-blue-600 hover:bg-blue-500 text-white text-xs px-2.5 py-1 md:px-3 md:py-1.5 rounded-full shadow-lg transition-colors"
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
