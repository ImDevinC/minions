import Link from "next/link";
import { MinionSummary, MinionStatus } from "@/types/minion";

interface StatusConfig {
  bg: string;
  text: string;
  label: string;
  pulse?: boolean;
}

const STATUS_CONFIGS: Record<MinionStatus, StatusConfig> = {
  pending: {
    bg: "bg-gray-500",
    text: "text-gray-200",
    label: "Pending",
  },
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
  completed: {
    bg: "bg-green-500",
    text: "text-green-200",
    label: "Completed",
  },
  failed: {
    bg: "bg-red-500",
    text: "text-red-200",
    label: "Failed",
  },
  terminated: {
    bg: "bg-orange-500",
    text: "text-orange-200",
    label: "Terminated",
  },
};

function StatusBadge({ status }: { status: MinionStatus }) {
  const config = STATUS_CONFIGS[status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium ${config.bg} ${config.text}`}
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

function formatRelativeTime(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHr = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHr / 24);

  if (diffSec < 60) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHr < 24) return `${diffHr}h ago`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return date.toLocaleDateString();
}

function truncateTask(task: string, maxLen = 120): string {
  if (task.length <= maxLen) return task;
  return task.slice(0, maxLen - 3) + "...";
}

export function MinionCard({ minion }: { minion: MinionSummary }) {
  const [owner, ...repoParts] = minion.repo.split("/");
  const repoName = repoParts.join("/");

  return (
    <Link
      href={`/minions/${minion.id}`}
      className="block bg-gray-800 hover:bg-gray-750 border border-gray-700 rounded-lg p-4 transition-colors"
    >
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1 min-w-0">
          {/* Repo */}
          <div className="flex items-center gap-2 text-sm text-gray-400 mb-1">
            <svg
              className="w-4 h-4 flex-shrink-0"
              fill="currentColor"
              viewBox="0 0 16 16"
            >
              <path d="M2 2.5A2.5 2.5 0 014.5 0h8.75a.75.75 0 01.75.75v12.5a.75.75 0 01-.75.75h-2.5a.75.75 0 110-1.5h1.75v-2h-8a1 1 0 00-.714 1.7.75.75 0 01-1.072 1.05A2.495 2.495 0 012 11.5v-9zm10.5-1V9h-8c-.356 0-.694.074-1 .208V2.5a1 1 0 011-1h8zM5 12.25v3.25a.25.25 0 00.4.2l1.45-1.087a.25.25 0 01.3 0L8.6 15.7a.25.25 0 00.4-.2v-3.25a.25.25 0 00-.25-.25h-3.5a.25.25 0 00-.25.25z" />
            </svg>
            <span className="truncate">
              <span className="text-gray-500">{owner}/</span>
              <span className="text-gray-300">{repoName}</span>
            </span>
          </div>

          {/* Task preview */}
          <p className="text-white text-sm line-clamp-2 mb-2">
            {truncateTask(minion.task)}
          </p>

          {/* Model and time */}
          <div className="flex items-center gap-3 text-xs text-gray-500">
            <span>{minion.model}</span>
            <span>·</span>
            <span>{formatRelativeTime(minion.created_at)}</span>
          </div>
        </div>

        {/* Status */}
        <StatusBadge status={minion.status} />
      </div>

      {/* Error or PR link indicator */}
      {minion.error && (
        <div className="mt-3 text-xs text-red-400 truncate">
          Error: {minion.error}
        </div>
      )}
      {minion.pr_url && (
        <div className="mt-3 flex items-center gap-1.5 text-xs text-green-400">
          <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 16 16">
            <path d="M1.5 3.25a2.25 2.25 0 113 2.122v5.256a2.251 2.251 0 11-1.5 0V5.372A2.25 2.25 0 011.5 3.25zm5.677-.177L9.573.677A.25.25 0 0110 .854V2.5h1A2.5 2.5 0 0113.5 5v5.628a2.251 2.251 0 11-1.5 0V5a1 1 0 00-1-1h-1v1.646a.25.25 0 01-.427.177L7.177 3.427a.25.25 0 010-.354zM3.75 2.5a.75.75 0 100 1.5.75.75 0 000-1.5zm0 9.5a.75.75 0 100 1.5.75.75 0 000-1.5zm8.25.75a.75.75 0 101.5 0 .75.75 0 00-1.5 0z" />
          </svg>
          PR created
        </div>
      )}
    </Link>
  );
}

export function MinionCardSkeleton() {
  return (
    <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 animate-pulse">
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1">
          <div className="h-4 bg-gray-700 rounded w-1/3 mb-2"></div>
          <div className="h-4 bg-gray-700 rounded w-full mb-1"></div>
          <div className="h-4 bg-gray-700 rounded w-2/3 mb-2"></div>
          <div className="h-3 bg-gray-700 rounded w-1/4"></div>
        </div>
        <div className="h-5 bg-gray-700 rounded-full w-16"></div>
      </div>
    </div>
  );
}
