"use client";

import { useState } from "react";

interface ThinkingBlockProps {
  /** The reasoning/thinking text content */
  content: string;
}

/**
 * ThinkingBlock renders collapsible reasoning content.
 *
 * Features:
 * - Default state is collapsed showing "▶ Thinking..."
 * - Clicking expands to show full reasoning text
 * - Collapsed state has muted, smaller styling
 */
export function ThinkingBlock({ content }: ThinkingBlockProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!content) {
    return null;
  }

  return (
    <div className="mb-3">
      <button
        type="button"
        onClick={() => setIsExpanded(!isExpanded)}
        className={`
          flex items-center gap-1.5 w-full text-left
          transition-colors duration-150
          ${isExpanded 
            ? "text-gray-300" 
            : "text-gray-500 hover:text-gray-400"
          }
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
        <span className={`text-sm ${isExpanded ? "" : "text-xs"}`}>
          Thinking...
        </span>
      </button>

      {isExpanded && (
        <div className="mt-2 pl-4 border-l-2 border-gray-700">
          <p className="text-sm text-gray-400 leading-relaxed whitespace-pre-wrap">
            {content}
          </p>
        </div>
      )}
    </div>
  );
}
