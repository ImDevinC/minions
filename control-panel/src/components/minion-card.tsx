"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import Chip from "@mui/material/Chip";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import Skeleton from "@mui/material/Skeleton";
import PullRequestIcon from "@mui/icons-material/CallMerge";
import ErrorIcon from "@mui/icons-material/ErrorOutlined";
import { MinionSummary, MinionStatus } from "@/types/minion";
import { PlatformBadge } from "./platform-icon";

interface StatusConfig {
  color: "default" | "primary" | "success" | "error" | "warning";
  label: string;
  pulse?: boolean;
}

const STATUS_CONFIGS: Record<MinionStatus, StatusConfig> = {
  pending: { color: "default", label: "Pending" },
  awaiting_clarification: { color: "warning", label: "Awaiting Clarification" },
  running: { color: "primary", label: "Running", pulse: true },
  completed: { color: "success", label: "Completed" },
  failed: { color: "error", label: "Failed" },
  terminated: { color: "warning", label: "Terminated" },
};

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
  return date.toLocaleDateString("en-US");
}

function useRelativeTime(dateString: string): string {
  const [relativeTime, setRelativeTime] = useState("");

  useEffect(() => {
    setRelativeTime(formatRelativeTime(dateString));
    const interval = setInterval(() => {
      setRelativeTime(formatRelativeTime(dateString));
    }, 60000);
    return () => clearInterval(interval);
  }, [dateString]);

  return relativeTime;
}

function truncateTask(task: string, maxLen = 120): string {
  if (task.length <= maxLen) return task;
  return task.slice(0, maxLen - 3) + "...";
}

function StatusChip({ status }: { status: MinionStatus }) {
  const config = STATUS_CONFIGS[status];
  return (
    <Chip
      size="small"
      color={config.color}
      label={config.label}
      sx={
        config.pulse
          ? {
              animation: "pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite",
              "@keyframes pulse": {
                "0%, 100%": { opacity: 1 },
                "50%": { opacity: 0.7 },
              },
            }
          : undefined
      }
    />
  );
}

export function MinionCard({ minion }: { minion: MinionSummary }) {
  const [owner, ...repoParts] = minion.repo.split("/");
  const repoName = repoParts.join("/");
  const relativeTime = useRelativeTime(minion.created_at);

  return (
    <Card
      component={Link}
      href={`/minions/${minion.id}`}
      sx={{
        textDecoration: "none",
        display: "block",
        height: "100%",
        transition: "transform 0.15s ease, box-shadow 0.15s ease",
        "&:hover": {
          transform: "translateY(-2px)",
          boxShadow: 6,
        },
      }}
    >
      <CardContent>
        <Box sx={{ display: "flex", justifyContent: "space-between", gap: 1, mb: 1 }}>
          <Box sx={{ minWidth: 0, flex: 1 }}>
            <Typography variant="body2" color="text.secondary" noWrap>
              <Box component="span" color="text.disabled">
                {owner}/
              </Box>
              {repoName}
            </Typography>
          </Box>
          <StatusChip status={minion.status} />
        </Box>

        <Typography variant="body1" color="text.primary" sx={{ mb: 2, minHeight: 48 }}>
          {truncateTask(minion.task)}
        </Typography>

        <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, minWidth: 0, flex: 1 }}>
            <Typography variant="caption" color="text.secondary" noWrap>
              {minion.model}
            </Typography>
            <Typography variant="caption" color="text.disabled">
              •
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {relativeTime}
            </Typography>
          </Box>
          <PlatformBadge platform={minion.platform} size="small" />
        </Box>

        {(minion.error || minion.pr_url) && (
          <Box sx={{ mt: 2, display: "flex", alignItems: "center", gap: 1 }}>
            {minion.error && (
              <Typography variant="caption" color="error" sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                <ErrorIcon fontSize="inherit" />
                Error: {minion.error}
              </Typography>
            )}
            {minion.pr_url && (
              <Typography variant="caption" color="success.main" sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                <PullRequestIcon fontSize="inherit" />
                PR created
              </Typography>
            )}
          </Box>
        )}
      </CardContent>
    </Card>
  );
}

export function MinionCardSkeleton() {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: "flex", justifyContent: "space-between", mb: 1 }}>
          <Skeleton width="40%" height={20} />
          <Skeleton width={60} height={24} variant="rounded" />
        </Box>
        <Skeleton width="100%" height={20} />
        <Skeleton width="66%" height={20} sx={{ mb: 2 }} />
        <Skeleton width="30%" height={16} />
      </CardContent>
    </Card>
  );
}
