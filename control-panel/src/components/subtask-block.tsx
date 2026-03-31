"use client";

import { useState } from "react";
import type { SubtaskThread, ChatMessage } from "@/types/minion";
import { ToolCallCard } from "./tool-call-card";
import { ThinkingBlock } from "./thinking-block";

interface SubtaskBlockProps {
  /** The subtask thread containing nested messages */
  subtask: SubtaskThread;
  /** ID of the currently expanded tool card (for accordion behavior) */
  expandedToolId: string | null;
  /** Callback when a tool card is toggled */
  onToolToggle: (toolId: string | null) => void;
}

/**
 * SubtaskBlock renders a nested conversation thread from a spawned subagent.
 *
 * Features:
 * - Default state collapsed with "▶ Subtask: [description]" header
 * - Clicking expands to show subtask messages inline (indented)
 * - Visual distinction via left border (cyan) and subtle background
 * - Recursively renders nested subtasks if they exist
 */
export function SubtaskBlock({
  subtask,
  expandedToolId,
  onToolToggle,
}: SubtaskBlockProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  // Build header text
  const agentLabel = subtask.agent ? ` (${subtask.agent})` : "";
  const headerText = `Subtask: ${subtask.description || "Task"}${agentLabel}`;

  return (
    <div className="mt-3 rounded-md bg-gray-800/40 border border-gray-700/50">
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className={`
          flex items-center gap-1.5 w-full text-left px-2 md:px-3 py-1.5 md:py-2
          transition-colors duration-150
          ${isExpanded ? "text-cyan-300" : "text-cyan-500 hover:text-cyan-400"}
        `}
      >
        <span
          className={`
            text-xs transition-transform duration-150
            ${isExpanded ? "rotate-90" : ""}
          `}
        >
          ▶
        </span>
        <span className="text-xs md:text-sm font-medium truncate min-w-0 flex-1">{headerText}</span>
        <span className="text-[10px] md:text-xs text-gray-500 flex-shrink-0">
          {subtask.messages.length} msg{subtask.messages.length !== 1 ? "s" : ""}
        </span>
      </button>

      {isExpanded && (
        <div className="border-t border-gray-700/50">
          <div className="pl-3 md:pl-4 border-l-2 border-cyan-600/50 ml-2 md:ml-3 my-2">
            {subtask.messages.length === 0 ? (
              <p className="text-xs text-gray-500 italic py-2 px-2">
                No messages yet
              </p>
            ) : (
              subtask.messages.map((message) => (
                <SubtaskMessageRow
                  key={message.id}
                  message={message}
                  expandedToolId={expandedToolId}
                  onToolToggle={onToolToggle}
                />
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * SubtaskMessageRow renders a single message within a subtask thread.
 * Simplified version of ChatMessageRow for nested context.
 */
function SubtaskMessageRow({
  message,
  expandedToolId,
  onToolToggle,
}: {
  message: ChatMessage;
  expandedToolId: string | null;
  onToolToggle: (toolId: string | null) => void;
}) {
  // Skip rendering if no meaningful content
  const hasContent =
    message.text.trim() ||
    message.thinking ||
    message.tools.length > 0 ||
    message.subtasks.length > 0;

  if (!hasContent) {
    return null;
  }

  return (
    <div className="py-2 px-2 border-b border-gray-700/30 last:border-b-0">
      {/* Timestamp */}
      <div className="text-xs text-gray-500 mb-1.5" suppressHydrationWarning>
        {new Date(message.timestamp).toLocaleTimeString()}
        {message.isStreaming && (
          <span className="ml-2 text-blue-400">
            <span className="inline-block w-1 h-1 bg-blue-400 rounded-full animate-pulse mr-1" />
            streaming
          </span>
        )}
      </div>

      {/* Collapsible thinking block */}
      {message.thinking && <ThinkingBlock content={message.thinking} />}

      {/* Text content - simplified, no full markdown in nested context */}
      {message.text.trim() && (
        <p className="text-sm text-gray-300 whitespace-pre-wrap mb-2">
          {message.text}
        </p>
      )}

      {/* Tool calls */}
      {message.tools.length > 0 && (
        <div className="mt-1.5 space-y-1">
          {message.tools.map((tool) => (
            <ToolCallCard
              key={tool.id}
              tool={tool}
              isExpanded={expandedToolId === tool.id}
              onToggle={() => {
                onToolToggle(expandedToolId === tool.id ? null : tool.id);
              }}
            />
          ))}
        </div>
      )}

      {/* Recursive subtasks */}
      {message.subtasks.length > 0 && (
        <div className="mt-2">
          {message.subtasks.map((nestedSubtask) => (
            <SubtaskBlock
              key={nestedSubtask.sessionID}
              subtask={nestedSubtask}
              expandedToolId={expandedToolId}
              onToolToggle={onToolToggle}
            />
          ))}
        </div>
      )}
    </div>
  );
}
