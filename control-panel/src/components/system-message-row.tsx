"use client";

import type { SystemMessage } from "@/types/minion";

/**
 * Format timestamp to readable time string.
 */
function formatTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/**
 * Extract retry attempt number from retry content.
 */
function getRetryAttempt(content: string | Record<string, unknown>): number {
  if (typeof content === "object" && content !== null) {
    const attempt = content.attempt ?? content.retry_count ?? content.retryCount;
    if (typeof attempt === "number") return attempt;
    // Sometimes it's nested
    const metadata = content.metadata as Record<string, unknown> | undefined;
    if (metadata) {
      const metaAttempt = metadata.attempt ?? metadata.retry_count;
      if (typeof metaAttempt === "number") return metaAttempt;
    }
  }
  return 0;
}

/**
 * Extract agent name from agent content.
 */
function getAgentName(content: string | Record<string, unknown>): string {
  if (typeof content === "string") return content;
  if (typeof content === "object" && content !== null) {
    const agent = content.agent ?? content.name ?? content.type;
    if (typeof agent === "string") return agent;
  }
  return "unknown";
}

/**
 * Extract error message from error content.
 */
function getErrorMessage(content: string | Record<string, unknown>): string {
  if (typeof content === "string") return content;
  if (typeof content === "object" && content !== null) {
    const error = content.error ?? content.message ?? content.reason;
    if (typeof error === "string") return error;
    // Fallback to JSON for complex errors
    return JSON.stringify(content, null, 2);
  }
  return "Unknown error";
}

interface SystemMessageRowProps {
  message: SystemMessage;
}

/**
 * SystemMessageRow renders system events as subtle banners.
 *
 * Styles:
 * - agent: "Switched to X agent" - subtle gray banner
 * - session.error: red error banner
 * - retry: warning banner with attempt number
 * - unknown: muted gray system message
 */
export function SystemMessageRow({ message }: SystemMessageRowProps) {
  const time = formatTime(message.timestamp);

  // Agent switch event
  if (message.type === "agent") {
    const agentName = getAgentName(message.content);
    return (
      <div className="flex items-center gap-2 py-1.5 px-4 text-xs text-gray-500 bg-gray-800/30">
        <span className="text-gray-600 shrink-0">{time}</span>
        <span className="text-gray-400">
          Switched to <span className="text-gray-300 font-medium">{agentName}</span> agent
        </span>
      </div>
    );
  }

  // Error event (session.error, error)
  if (message.type === "session.error" || message.type === "error") {
    const errorMsg = getErrorMessage(message.content);
    return (
      <div className="flex items-start gap-2 py-2 px-4 text-sm bg-red-500/10 border-l-2 border-red-500">
        <span className="text-red-400/80 shrink-0 text-xs">{time}</span>
        <span className="text-red-400 whitespace-pre-wrap break-words">{errorMsg}</span>
      </div>
    );
  }

  // Retry event
  if (message.type === "retry") {
    const attempt = getRetryAttempt(message.content);
    const attemptText = attempt > 0 ? `, attempt ${attempt}` : "";
    return (
      <div className="flex items-center gap-2 py-1.5 px-4 text-xs bg-yellow-500/10 border-l-2 border-yellow-500">
        <span className="text-yellow-400/70 shrink-0">{time}</span>
        <span className="text-yellow-400">⚠️ Retrying{attemptText}</span>
      </div>
    );
  }

  // Unknown/fallback system message - muted and subtle
  const displayContent =
    typeof message.content === "string"
      ? message.content
      : JSON.stringify(message.content);

  return (
    <div className="flex items-center gap-2 py-1 px-4 text-xs text-gray-600 bg-gray-800/20">
      <span className="text-gray-700 shrink-0">{time}</span>
      <span className="text-gray-500 truncate">{displayContent}</span>
    </div>
  );
}
