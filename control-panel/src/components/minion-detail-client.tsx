"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { MinionDetail, MinionStatus } from "@/types/minion";
import { TerminateModal } from "./terminate-modal";
import { useMinionEvents } from "@/hooks/use-minion-events";
import { ChatView } from "./chat-view";

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

  // Use WebSocket hook for live event updates
  const { events, isConnected, connectionError, isCatchingUp } = useMinionEvents({
    minionId: minion.id,
    initialEvents: minion.events,
    status: currentStatus,
  });

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

      {/* Chat view section */}
      <div>
        {(!isTerminalStatus(currentStatus) || connectionError) && (
          <div className="flex items-center justify-end gap-2 mb-3">
            {/* Connection status indicator */}
            {!isTerminalStatus(currentStatus) && (
              <div className="flex items-center gap-1.5 text-xs">
                <span
                  className={`inline-block w-2 h-2 rounded-full ${
                    isConnected ? "bg-green-500" : "bg-yellow-500 animate-pulse"
                  }`}
                />
                <span className="text-gray-400">
                  {isConnected ? "Live" : "Connecting..."}
                </span>
              </div>
            )}
            {connectionError && (
              <span className="text-xs text-red-400">{connectionError}</span>
            )}
          </div>
        )}
        <ChatView 
          events={events}
          status={currentStatus}
          isConnected={isConnected}
          isCatchingUp={isCatchingUp}
        />
      </div>
    </div>
  );
}

// Check if status is terminal (helper for render)
function isTerminalStatus(status: MinionStatus): boolean {
  return (
    status === "completed" || status === "failed" || status === "terminated"
  );
}
