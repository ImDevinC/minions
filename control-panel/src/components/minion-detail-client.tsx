"use client";

import { useRef, useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { useVirtualizer } from "@tanstack/react-virtual";
import { MinionDetail, MinionEvent, MinionStatus } from "@/types/minion";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import { TerminateModal } from "./terminate-modal";

// Re-export StatusBadge for server component compatibility
interface StatusConfig {
  bg: string;
  text: string;
  label: string;
  pulse?: boolean;
}

const STATUS_CONFIGS: Record<MinionStatus, StatusConfig> = {
  pending: { bg: "bg-gray-500", text: "text-gray-200", label: "Pending" },
  awaiting_clarification: {
    bg: "bg-yellow-500",
    text: "text-yellow-200",
    label: "Awaiting Clarification",
  },
  running: {
    bg: "bg-blue-500",
    text: "text-blue-200",
    label: "Running",
    pulse: true,
  },
  completed: { bg: "bg-green-500", text: "text-green-200", label: "Completed" },
  failed: { bg: "bg-red-500", text: "text-red-200", label: "Failed" },
  terminated: {
    bg: "bg-orange-500",
    text: "text-orange-200",
    label: "Terminated",
  },
};

export function StatusBadge({ status }: { status: MinionStatus }) {
  const config = STATUS_CONFIGS[status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${config.bg} ${config.text}`}
    >
      {config.pulse && (
        <span className="relative flex h-2 w-2">
          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-current opacity-75"></span>
          <span className="relative inline-flex rounded-full h-2 w-2 bg-current"></span>
        </span>
      )}
      {config.label}
    </span>
  );
}

// Format cost in USD
function formatCost(costUsd: number): string {
  if (costUsd < 0.01) {
    return `$${costUsd.toFixed(4)}`;
  }
  return `$${costUsd.toFixed(2)}`;
}

// Format token counts
function formatTokens(count: number): string {
  if (count >= 1_000_000) {
    return `${(count / 1_000_000).toFixed(1)}M`;
  }
  if (count >= 1_000) {
    return `${(count / 1_000).toFixed(1)}K`;
  }
  return count.toString();
}

// Format timestamp for event log
function formatEventTime(timestamp: string): string {
  const date = new Date(timestamp);
  return date.toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

// Detect code blocks in content and extract language
interface CodeBlock {
  language: string;
  code: string;
}

function extractCodeBlocks(content: Record<string, unknown>): CodeBlock[] {
  const blocks: CodeBlock[] = [];

  // Check common patterns for code in events
  const codeFields = ["code", "content", "output", "message", "text", "data"];

  for (const field of codeFields) {
    const value = content[field];
    if (typeof value === "string" && value.includes("```")) {
      // Extract fenced code blocks
      const regex = /```(\w*)\n?([\s\S]*?)```/g;
      let match;
      while ((match = regex.exec(value)) !== null) {
        blocks.push({
          language: match[1] || "text",
          code: match[2].trim(),
        });
      }
    }
  }

  return blocks;
}

// Determine if content looks like code
function looksLikeCode(content: string): boolean {
  const codeIndicators = [
    /^(import|export|const|let|var|function|class|interface|type)\s/m,
    /^(def|class|import|from|return)\s/m,
    /^(package|func|type|import)\s/m,
    /[{};]\s*$/m,
    /^\s*(\/\/|#|\/\*)/m,
  ];
  return codeIndicators.some((re) => re.test(content));
}

// Guess language from content
function guessLanguage(content: string): string {
  if (/^(import|export|const|let|interface|type)\s/m.test(content))
    return "typescript";
  if (/^(def|class|import|from)\s.*:/m.test(content)) return "python";
  if (/^(package|func|type)\s/m.test(content)) return "go";
  if (/^(fn|let|mut|impl|use)\s/m.test(content)) return "rust";
  return "text";
}

interface EventRowProps {
  event: MinionEvent;
}

function EventRow({ event }: EventRowProps) {
  const [expanded, setExpanded] = useState(false);
  const codeBlocks = extractCodeBlocks(event.content);

  // Helper to safely extract string from unknown content
  const stringify = (value: unknown): string => {
    if (typeof value === "string") return value;
    if (value === null || value === undefined) return "";
    return JSON.stringify(value);
  };

  // Get display content
  const displayContent: string = (() => {
    // Handle different event types
    if (event.event_type === "message" || event.event_type === "assistant") {
      const content = event.content.content || event.content.message || "";
      return stringify(content);
    }
    if (event.event_type === "tool_use" || event.event_type === "tool_call") {
      const tool = event.content.tool || event.content.name || "tool";
      return `Tool: ${stringify(tool)}`;
    }
    if (event.event_type === "tool_result") {
      return "Tool result";
    }
    if (event.event_type === "error") {
      const msg = event.content.message || event.content.error;
      return msg ? stringify(msg) : "Error occurred";
    }
    if (event.event_type === "token_usage") {
      const input = event.content.input_tokens || 0;
      const output = event.content.output_tokens || 0;
      return `Tokens: ${input} in, ${output} out`;
    }
    // Fallback: show type and summary
    const summary = event.content.summary || event.content.message;
    return summary
      ? stringify(summary)
      : JSON.stringify(event.content).slice(0, 200);
  })();

  const hasDetails =
    codeBlocks.length > 0 ||
    JSON.stringify(event.content).length > 200 ||
    looksLikeCode(displayContent);

  return (
    <div className="border-b border-gray-800 py-2 px-3 hover:bg-gray-800/50 transition-colors">
      <div className="flex items-start gap-3">
        {/* Timestamp */}
        <span className="text-xs text-gray-500 font-mono whitespace-nowrap pt-0.5">
          {formatEventTime(event.timestamp)}
        </span>

        {/* Event type badge */}
        <span
          className={`text-xs px-1.5 py-0.5 rounded whitespace-nowrap ${
            event.event_type === "error"
              ? "bg-red-500/20 text-red-400"
              : event.event_type === "message" ||
                  event.event_type === "assistant"
                ? "bg-blue-500/20 text-blue-400"
                : event.event_type === "tool_use" ||
                    event.event_type === "tool_call"
                  ? "bg-purple-500/20 text-purple-400"
                  : event.event_type === "tool_result"
                    ? "bg-green-500/20 text-green-400"
                    : "bg-gray-500/20 text-gray-400"
          }`}
        >
          {event.event_type}
        </span>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <div
            className={`text-sm text-gray-300 ${!expanded ? "line-clamp-2" : ""}`}
          >
            {displayContent}
          </div>

          {/* Expanded content with code blocks */}
          {expanded && codeBlocks.length > 0 && (
            <div className="mt-2 space-y-2">
              {codeBlocks.map((block, i) => (
                <div
                  key={i}
                  className="rounded overflow-hidden text-xs border border-gray-700"
                >
                  <SyntaxHighlighter
                    language={block.language}
                    style={oneDark}
                    customStyle={{
                      margin: 0,
                      padding: "0.75rem",
                      background: "#1a1a2e",
                      fontSize: "0.75rem",
                    }}
                    wrapLines
                    wrapLongLines
                  >
                    {block.code}
                  </SyntaxHighlighter>
                </div>
              ))}
            </div>
          )}

          {/* Expanded raw JSON */}
          {expanded && codeBlocks.length === 0 && hasDetails && (
            <div className="mt-2 rounded overflow-hidden text-xs border border-gray-700">
              <SyntaxHighlighter
                language="json"
                style={oneDark}
                customStyle={{
                  margin: 0,
                  padding: "0.75rem",
                  background: "#1a1a2e",
                  fontSize: "0.75rem",
                }}
                wrapLines
                wrapLongLines
              >
                {JSON.stringify(event.content, null, 2)}
              </SyntaxHighlighter>
            </div>
          )}
        </div>

        {/* Expand button */}
        {hasDetails && (
          <button
            onClick={() => setExpanded(!expanded)}
            className="text-xs text-gray-500 hover:text-gray-300 whitespace-nowrap"
          >
            {expanded ? "▲ Less" : "▼ More"}
          </button>
        )}
      </div>
    </div>
  );
}

interface EventLogProps {
  events: MinionEvent[];
}

export function EventLog({ events }: EventLogProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  // Sort events by timestamp ascending (oldest first)
  const sortedEvents = [...events].sort(
    (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );

  const virtualizer = useVirtualizer({
    count: sortedEvents.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 60, // Estimated row height
    overscan: 10, // Render 10 extra items for smoother scrolling
  });

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    if (autoScroll && sortedEvents.length > 0) {
      virtualizer.scrollToIndex(sortedEvents.length - 1, { align: "end" });
    }
  }, [sortedEvents.length, autoScroll, virtualizer]);

  // Detect manual scroll to disable auto-scroll
  const handleScroll = useCallback(() => {
    if (!parentRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = parentRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 100;
    setAutoScroll(isAtBottom);
  }, []);

  if (sortedEvents.length === 0) {
    return (
      <div className="flex items-center justify-center h-64 text-gray-500">
        No events yet
      </div>
    );
  }

  return (
    <div className="relative">
      {/* Auto-scroll indicator */}
      {!autoScroll && (
        <button
          onClick={() => {
            setAutoScroll(true);
            virtualizer.scrollToIndex(sortedEvents.length - 1, {
              align: "end",
            });
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
          {virtualizer.getVirtualItems().map((virtualRow) => (
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
              <EventRow event={sortedEvents[virtualRow.index]} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// Check if a minion can be terminated (is in a running state)
function canTerminate(status: MinionStatus): boolean {
  return status === "pending" || status === "running" || status === "awaiting_clarification";
}

interface MinionDetailClientProps {
  minion: MinionDetail;
}

export function MinionDetailClient({ minion }: MinionDetailClientProps) {
  const router = useRouter();
  const [showTerminateModal, setShowTerminateModal] = useState(false);
  const [currentStatus, setCurrentStatus] = useState(minion.status);

  // Update status when minion prop changes
  useEffect(() => {
    setCurrentStatus(minion.status);
  }, [minion.status]);

  const handleTerminate = useCallback(async () => {
    const response = await fetch(`/api/minions/${minion.id}/terminate`, {
      method: "POST",
    });

    if (!response.ok) {
      const data = await response.json();
      throw new Error(data.error || "Failed to terminate minion");
    }

    // Update local status and refresh the page data
    setCurrentStatus("terminated");
    router.refresh();
  }, [minion.id, router]);

  return (
    <div className="space-y-6">
      {/* Terminate modal */}
      <TerminateModal
        isOpen={showTerminateModal}
        onClose={() => setShowTerminateModal(false)}
        onConfirm={handleTerminate}
        minionId={minion.id}
        repo={minion.repo}
      />

      {/* Header section */}
      <div className="bg-gray-800 border border-gray-700 rounded-lg p-6">
        <div className="flex items-start justify-between gap-4 mb-4">
          <div>
            <h1 className="text-2xl font-bold text-white mb-2">{minion.repo}</h1>
            <div className="flex items-center gap-3">
              <StatusBadge status={currentStatus} />
              {canTerminate(currentStatus) && (
                <button
                  onClick={() => setShowTerminateModal(true)}
                  className="inline-flex items-center gap-1.5 px-3 py-1 text-xs font-medium text-red-400 bg-red-500/10 border border-red-500/30 rounded-full hover:bg-red-500/20 transition-colors"
                >
                  <svg
                    className="w-3.5 h-3.5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M6 18L18 6M6 6l12 12"
                    />
                  </svg>
                  Terminate
                </button>
              )}
            </div>
          </div>

          {/* Cost and tokens */}
          <div className="text-right">
            <div className="text-2xl font-bold text-green-400">
              {formatCost(minion.cost_usd)}
            </div>
            <div className="text-xs text-gray-500">
              {formatTokens(minion.input_tokens)} in /{" "}
              {formatTokens(minion.output_tokens)} out
            </div>
          </div>
        </div>

        {/* Task */}
        <div className="mb-4">
          <h2 className="text-sm font-medium text-gray-400 mb-1">Task</h2>
          <p className="text-gray-200 whitespace-pre-wrap">{minion.task}</p>
        </div>

        {/* Metadata grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div>
            <span className="text-gray-500">Model</span>
            <p className="text-gray-200">{minion.model}</p>
          </div>
          <div>
            <span className="text-gray-500">Created</span>
            <p className="text-gray-200">
              {new Date(minion.created_at).toLocaleString()}
            </p>
          </div>
          {minion.started_at && (
            <div>
              <span className="text-gray-500">Started</span>
              <p className="text-gray-200">
                {new Date(minion.started_at).toLocaleString()}
              </p>
            </div>
          )}
          {minion.completed_at && (
            <div>
              <span className="text-gray-500">Completed</span>
              <p className="text-gray-200">
                {new Date(minion.completed_at).toLocaleString()}
              </p>
            </div>
          )}
        </div>

        {/* PR link */}
        {minion.pr_url && (
          <div className="mt-4 p-3 bg-green-500/10 border border-green-500/30 rounded-lg">
            <div className="flex items-center gap-2">
              <svg
                className="w-5 h-5 text-green-400"
                fill="currentColor"
                viewBox="0 0 16 16"
              >
                <path d="M1.5 3.25a2.25 2.25 0 113 2.122v5.256a2.251 2.251 0 11-1.5 0V5.372A2.25 2.25 0 011.5 3.25zm5.677-.177L9.573.677A.25.25 0 0110 .854V2.5h1A2.5 2.5 0 0113.5 5v5.628a2.251 2.251 0 11-1.5 0V5a1 1 0 00-1-1h-1v1.646a.25.25 0 01-.427.177L7.177 3.427a.25.25 0 010-.354zM3.75 2.5a.75.75 0 100 1.5.75.75 0 000-1.5zm0 9.5a.75.75 0 100 1.5.75.75 0 000-1.5zm8.25.75a.75.75 0 101.5 0 .75.75 0 00-1.5 0z" />
              </svg>
              <a
                href={minion.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-green-400 hover:text-green-300 hover:underline"
              >
                {minion.pr_url}
              </a>
            </div>
          </div>
        )}

        {/* Error */}
        {minion.error && (
          <div className="mt-4 p-3 bg-red-500/10 border border-red-500/30 rounded-lg">
            <div className="flex items-start gap-2">
              <svg
                className="w-5 h-5 text-red-400 flex-shrink-0 mt-0.5"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              <span className="text-red-400">{minion.error}</span>
            </div>
          </div>
        )}

        {/* Clarification */}
        {minion.clarification_question && (
          <div className="mt-4 p-3 bg-yellow-500/10 border border-yellow-500/30 rounded-lg">
            <h3 className="text-sm font-medium text-yellow-400 mb-1">
              Clarification Question
            </h3>
            <p className="text-gray-200">{minion.clarification_question}</p>
            {minion.clarification_answer && (
              <>
                <h3 className="text-sm font-medium text-yellow-400 mt-3 mb-1">
                  Answer
                </h3>
                <p className="text-gray-200">{minion.clarification_answer}</p>
              </>
            )}
          </div>
        )}
      </div>

      {/* Event log section */}
      <div>
        <h2 className="text-lg font-semibold text-white mb-3">
          Event Log ({minion.events.length} events)
        </h2>
        <EventLog events={minion.events} />
      </div>
    </div>
  );
}
