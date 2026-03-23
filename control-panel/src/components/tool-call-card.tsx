"use client";

import { useMemo, useState } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import type { ToolCall, ToolCallStatus } from "@/types/minion";

/**
 * Truncation constants:
 * - SIZE_THRESHOLD: 10KB - content larger than this gets truncated
 * - PREVIEW_LENGTH: ~500 chars shown in truncated preview
 */
const SIZE_THRESHOLD = 10 * 1024; // 10KB
const PREVIEW_LENGTH = 500;

/**
 * Tool icon mapping based on tool type.
 * Fallback: 🔧 for unknown tools
 */
const TOOL_ICONS: Record<string, string> = {
  read: "📖",
  write: "✏️",
  edit: "✏️",
  glob: "🔍",
  grep: "🔎",
  bash: "⚙️",
  task: "🤖",
  webfetch: "🌐",
};

function getToolIcon(tool: string): string {
  return TOOL_ICONS[tool.toLowerCase()] || "🔧";
}

/**
 * Generate human-readable summary from tool input/output.
 * Spec:
 * - Read: "Read <filepath>"
 * - Write: "Wrote <filepath>"
 * - Edit: "Edited <filepath>"
 * - Glob: "Found N files"
 * - Grep: "Found N matches"
 * - Bash: "Ran: <truncated command>"
 * - Task: "Spawned <type> agent"
 * - Unknown: fall back to tool name
 */
export function getToolSummary(tool: ToolCall): string {
  const toolName = tool.tool.toLowerCase();
  const input = tool.input || {};

  switch (toolName) {
    case "read": {
      const filePath = (input.filePath || input.file_path || input.path) as string | undefined;
      return filePath ? `Read ${filePath}` : "Read file";
    }
    case "write": {
      const filePath = (input.filePath || input.file_path || input.path) as string | undefined;
      return filePath ? `Wrote ${filePath}` : "Wrote file";
    }
    case "edit": {
      const filePath = (input.filePath || input.file_path || input.path) as string | undefined;
      return filePath ? `Edited ${filePath}` : "Edited file";
    }
    case "glob": {
      // Try to parse output for match count, fall back to showing pattern
      if (tool.output) {
        const lines = tool.output.trim().split("\n").filter((l) => l.length > 0);
        return `Found ${lines.length} files`;
      }
      const pattern = (input.pattern || input.glob) as string | undefined;
      return pattern ? `Glob: ${pattern}` : "Finding files";
    }
    case "grep": {
      // Try to parse output for match count
      if (tool.output) {
        const lines = tool.output.trim().split("\n").filter((l) => l.length > 0);
        return `Found ${lines.length} matches`;
      }
      const pattern = (input.pattern || input.query || input.search) as string | undefined;
      return pattern ? `Grep: ${pattern}` : "Searching";
    }
    case "bash": {
      const command = (input.command || input.cmd) as string | undefined;
      if (command) {
        // Truncate long commands (show first ~40 chars)
        const truncated = command.length > 40 ? command.slice(0, 40) + "..." : command;
        return `Ran: ${truncated}`;
      }
      return "Ran command";
    }
    case "task":
    case "agent": {
      const agentType = (input.subagent_type || input.agent_type || input.type || input.agent) as string | undefined;
      return agentType ? `Spawned ${agentType} agent` : "Spawned agent";
    }
    case "webfetch": {
      const url = (input.url) as string | undefined;
      if (url) {
        // Truncate long URLs
        const truncated = url.length > 40 ? url.slice(0, 40) + "..." : url;
        return `Fetched ${truncated}`;
      }
      return "Fetched URL";
    }
    case "todowrite": {
      const todos = (input.todos) as unknown[] | undefined;
      if (Array.isArray(todos)) {
        return `Updated ${todos.length} todos`;
      }
      return "Updated todos";
    }
    case "skill": {
      const name = (input.name) as string | undefined;
      return name ? `Loaded skill: ${name}` : "Loaded skill";
    }
    default:
      // Fall back to just the tool name, capitalized
      return tool.tool.charAt(0).toUpperCase() + tool.tool.slice(1);
  }
}

/**
 * Status badge colors:
 * - pending: gray
 * - running: blue with pulse animation
 * - completed: green
 * - error: red
 */
function getStatusBadgeClasses(status: ToolCallStatus): string {
  switch (status) {
    case "pending":
      return "bg-gray-600 text-gray-200";
    case "running":
      return "bg-blue-600 text-blue-100 animate-pulse";
    case "completed":
      return "bg-green-700 text-green-100";
    case "error":
      return "bg-red-700 text-red-100";
    default:
      return "bg-gray-600 text-gray-200";
  }
}

interface ToolCallCardProps {
  tool: ToolCall;
  isExpanded: boolean;
  onToggle: () => void;
}

/**
 * TruncatedContent handles display of potentially large text content.
 * If content exceeds SIZE_THRESHOLD, shows truncated preview with expand button.
 */
function TruncatedContent({
  content,
  label,
  className,
}: {
  content: string;
  label: string;
  className?: string;
}) {
  const [showFull, setShowFull] = useState(false);
  const isLarge = content.length > SIZE_THRESHOLD;
  const displayContent = isLarge && !showFull
    ? content.slice(0, PREVIEW_LENGTH) + "..."
    : content;

  return (
    <div>
      <pre className={className}>
        {displayContent}
      </pre>
      {isLarge && (
        <button
          type="button"
          onClick={() => setShowFull(!showFull)}
          className="mt-2 text-xs text-blue-400 hover:text-blue-300 transition-colors"
        >
          {showFull ? `Hide full ${label}` : `Show full ${label}`}
        </button>
      )}
    </div>
  );
}

/**
 * TruncatedSyntaxHighlighter handles large JSON inputs with syntax highlighting.
 * If content exceeds SIZE_THRESHOLD, shows truncated preview with expand button.
 */
function TruncatedSyntaxHighlighter({
  content,
  label,
}: {
  content: string;
  label: string;
}) {
  const [showFull, setShowFull] = useState(false);
  const isLarge = content.length > SIZE_THRESHOLD;
  const displayContent = isLarge && !showFull
    ? content.slice(0, PREVIEW_LENGTH) + "..."
    : content;

  return (
    <div>
      <div className="rounded overflow-hidden border border-gray-700">
        <div className="overflow-x-auto">
          <SyntaxHighlighter
            language="json"
            style={oneDark}
            customStyle={{
              margin: 0,
              padding: "0.75rem",
              background: "#1a1a2e",
              fontSize: "0.75rem",
              whiteSpace: "pre",
              overflowX: "auto",
            }}
            wrapLines={false}
            wrapLongLines={false}
          >
            {displayContent}
          </SyntaxHighlighter>
        </div>
      </div>
      {isLarge && (
        <button
          type="button"
          onClick={() => setShowFull(!showFull)}
          className="mt-2 text-xs text-blue-400 hover:text-blue-300 transition-colors"
        >
          {showFull ? `Hide full ${label}` : `Show full ${label}`}
        </button>
      )}
    </div>
  );
}

/**
 * ToolCallCard renders a compact expandable tool invocation card.
 *
 * Features:
 * - Compact single-line card showing icon, tool name, status badge, summary
 * - Click expands to show full input (as syntax-highlighted JSON) and output
 * - Status badge colors: pending (gray), running (blue pulse), completed (green), error (red)
 */
export function ToolCallCard({ tool, isExpanded, onToggle }: ToolCallCardProps) {
  // Memoize JSON stringification
  const inputJson = useMemo(() => {
    try {
      return JSON.stringify(tool.input, null, 2);
    } catch {
      return String(tool.input);
    }
  }, [tool.input]);

  // Generate human-readable summary (memoized based on tool data)
  const summary = useMemo(() => getToolSummary(tool), [tool]);

  const icon = getToolIcon(tool.tool);
  const statusClasses = getStatusBadgeClasses(tool.status);

  return (
    <div className="my-2 rounded-lg border border-gray-700 bg-gray-900/50 overflow-hidden">
      {/* Compact header - always visible */}
      <button
        type="button"
        onClick={onToggle}
        className="w-full flex items-center gap-1.5 md:gap-2 px-2 md:px-3 py-1.5 md:py-2 text-left hover:bg-gray-800/50 transition-colors"
      >
        {/* Icon */}
        <span className="text-sm md:text-base flex-shrink-0" role="img" aria-label={tool.tool}>
          {icon}
        </span>

        {/* Tool name - hidden on mobile to save space, summary provides context */}
        <span className="hidden sm:inline text-sm font-medium text-gray-200 flex-shrink-0">
          {tool.tool}
        </span>

        {/* Status badge */}
        <span
          className={`px-1 md:px-1.5 py-0.5 rounded text-[10px] md:text-xs font-medium flex-shrink-0 ${statusClasses}`}
        >
          {tool.status}
        </span>

        {/* Summary - truncated, smaller on mobile */}
        <span className="text-xs md:text-sm text-gray-400 truncate flex-1 min-w-0">
          {summary}
        </span>

        {/* Expand indicator */}
        <span
          className={`text-xs text-gray-500 transition-transform duration-150 flex-shrink-0 ${
            isExpanded ? "rotate-90" : ""
          }`}
        >
          ▶
        </span>
      </button>

      {/* Expanded content - input and output with smooth collapse animation */}
      <div
        className={`grid transition-[grid-template-rows] duration-200 ease-out ${
          isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
        }`}
      >
        <div className="overflow-hidden">
          <div className={`border-t border-gray-700 ${isExpanded ? "opacity-100" : "opacity-0"} transition-opacity duration-200`}>
            {/* Tool input as syntax-highlighted JSON */}
            <div className="px-2 md:px-3 py-2 border-b border-gray-700">
              <div className="text-xs text-gray-500 mb-1">Input</div>
              <TruncatedSyntaxHighlighter content={inputJson} label="input" />
            </div>

            {/* Tool output (if present) */}
            {tool.output && (
              <div className="px-2 md:px-3 py-2 border-b border-gray-700">
                <div className="text-xs text-gray-500 mb-1">Output</div>
                <TruncatedContent
                  content={tool.output}
                  label="output"
                  className="text-xs text-gray-300 bg-gray-800/50 rounded p-2 overflow-x-auto whitespace-pre-wrap break-words max-h-64 overflow-y-auto"
                />
              </div>
            )}

            {/* Error (if present) */}
            {tool.error && (
              <div className="px-2 md:px-3 py-2">
                <div className="text-xs text-red-400 mb-1">Error</div>
                <pre className="text-xs text-red-300 bg-red-900/20 rounded p-2 overflow-x-auto whitespace-pre-wrap break-words">
                  {tool.error}
                </pre>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
