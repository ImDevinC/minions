"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { MinionDetail, MinionStatus } from "@/types/minion";
import { TerminateModal } from "./terminate-modal";
import { useMinionEvents } from "@/hooks/use-minion-events";
import { ChatView } from "./chat-view";
import { PlatformBadge } from "./platform-icon";
import Paper from "@mui/material/Paper";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Grid from "@mui/material/Grid";
import Alert from "@mui/material/Alert";
import Divider from "@mui/material/Divider";
import StopIcon from "@mui/icons-material/Stop";
import PullRequestIcon from "@mui/icons-material/CallMerge";

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

export function StatusChip({ status }: { status: MinionStatus }) {
  const config = STATUS_CONFIGS[status];
  return (
    <Chip
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

function formatCost(costUsd: number): string {
  return `$${costUsd.toFixed(5)}`;
}

function formatTokens(count: number): string {
  return count.toLocaleString("en-US");
}

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
  const [currentCost, setCurrentCost] = useState(minion.cost_usd);
  const [currentInputTokens, setCurrentInputTokens] = useState(minion.input_tokens);
  const [currentOutputTokens, setCurrentOutputTokens] = useState(minion.output_tokens);

  const { events, isConnected, connectionError, isCatchingUp } = useMinionEvents({
    minionId: minion.id,
    initialEvents: minion.events,
    status: currentStatus,
  });

  useEffect(() => {
    setCurrentStatus(minion.status);
  }, [minion.status]);

  useEffect(() => {
    if (
      currentStatus === "completed" ||
      currentStatus === "failed" ||
      currentStatus === "terminated"
    ) {
      return;
    }

    const pollInterval = setInterval(async () => {
      try {
        const response = await fetch(`/api/minions/${minion.id}`);
        if (response.ok) {
          const data = await response.json();
          setCurrentCost(data.cost_usd);
          setCurrentInputTokens(data.input_tokens);
          setCurrentOutputTokens(data.output_tokens);
          setCurrentStatus(data.status);
        }
      } catch (error) {
        console.error("Failed to poll minion data:", error);
      }
    }, 3000);

    return () => clearInterval(pollInterval);
  }, [minion.id, currentStatus]);

  const handleTerminate = useCallback(async () => {
    const response = await fetch(`/api/minions/${minion.id}/terminate`, {
      method: "POST",
    });

    if (!response.ok) {
      const data = await response.json();
      throw new Error(data.error || "Failed to terminate minion");
    }

    setCurrentStatus("terminated");
    router.refresh();
  }, [minion.id, router]);

  const isTerminalStatus =
    currentStatus === "completed" || currentStatus === "failed" || currentStatus === "terminated";

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <TerminateModal
        isOpen={showTerminateModal}
        onClose={() => setShowTerminateModal(false)}
        onConfirm={handleTerminate}
        minionId={minion.id}
        repo={minion.repo}
      />

      <Paper elevation={2} sx={{ p: 3 }}>
        <Box
          sx={{
            display: "flex",
            flexDirection: { xs: "column", sm: "row" },
            justifyContent: "space-between",
            gap: 2,
            mb: 2,
          }}
        >
          <Box>
            <Typography
              variant="h5"
              component="h1"
              sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1, wordBreak: "break-all" }}
            >
              <PlatformBadge platform={minion.platform} size="medium" />
              {minion.repo}
            </Typography>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              <StatusChip status={currentStatus} />
              {canTerminate(currentStatus) && (
                <Button
                  variant="outlined"
                  color="error"
                  size="small"
                  startIcon={<StopIcon />}
                  onClick={() => setShowTerminateModal(true)}
                >
                  Terminate
                </Button>
              )}
            </Box>
          </Box>
          <Box sx={{ textAlign: { sm: "right" } }}>
            <Typography variant="h4" color="success.main">
              {formatCost(currentCost)}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {formatTokens(currentInputTokens)} in / {formatTokens(currentOutputTokens)} out
            </Typography>
          </Box>
        </Box>

        <Divider sx={{ my: 2 }} />

        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle2" color="text.secondary" gutterBottom>
            Task
          </Typography>
          <Typography variant="body1" sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
            {minion.task}
          </Typography>
        </Box>

        <Grid container spacing={2}>
          <Grid size={{ xs: 12, sm: 6, md: 3 }}>
            <Typography variant="subtitle2" color="text.secondary">
              Model
            </Typography>
            <Typography variant="body2" sx={{ wordBreak: "break-all" }}>
              {minion.model}
            </Typography>
          </Grid>
          <Grid size={{ xs: 12, sm: 6, md: 3 }}>
            <Typography variant="subtitle2" color="text.secondary">
              Created
            </Typography>
            <Typography variant="body2" suppressHydrationWarning>
              {new Date(minion.created_at).toLocaleString()}
            </Typography>
          </Grid>
          {minion.started_at && (
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <Typography variant="subtitle2" color="text.secondary">
                Started
              </Typography>
              <Typography variant="body2" suppressHydrationWarning>
                {new Date(minion.started_at).toLocaleString()}
              </Typography>
            </Grid>
          )}
          {minion.completed_at && (
            <Grid size={{ xs: 12, sm: 6, md: 3 }}>
              <Typography variant="subtitle2" color="text.secondary">
                Completed
              </Typography>
              <Typography variant="body2" suppressHydrationWarning>
                {new Date(minion.completed_at).toLocaleString()}
              </Typography>
            </Grid>
          )}
        </Grid>

        {minion.pr_url && (
          <Alert severity="success" sx={{ mt: 3 }} icon={<PullRequestIcon fontSize="inherit" />}>
            <Typography
              variant="body2"
              component="a"
              href={minion.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              color="inherit"
              sx={{ wordBreak: "break-all" }}
            >
              {minion.pr_url}
            </Typography>
          </Alert>
        )}

        {minion.error && (
          <Alert severity="error" sx={{ mt: 3 }}>
            <Typography variant="body2" sx={{ wordBreak: "break-word" }}>
              {minion.error}
            </Typography>
          </Alert>
        )}

        {minion.clarification_question && (
          <Alert severity="warning" sx={{ mt: 3 }}>
            <Typography variant="subtitle2" gutterBottom>
              Clarification Question
            </Typography>
            <Typography variant="body2" sx={{ wordBreak: "break-word" }}>
              {minion.clarification_question}
            </Typography>
            {minion.clarification_answer && (
              <>
                <Typography variant="subtitle2" gutterBottom sx={{ mt: 1 }}>
                  Answer
                </Typography>
                <Typography variant="body2" sx={{ wordBreak: "break-word" }}>
                  {minion.clarification_answer}
                </Typography>
              </>
            )}
          </Alert>
        )}
      </Paper>

      <Box>
        {(!isTerminalStatus || connectionError) && (
          <Box
            sx={{ display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 2, mb: 1 }}
          >
            {!isTerminalStatus && (
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Box
                  sx={{
                    width: 8,
                    height: 8,
                    borderRadius: "50%",
                    backgroundColor: isConnected ? "success.main" : "warning.main",
                    animation: isConnected ? "none" : "pulse 1.5s infinite",
                  }}
                />
                <Typography variant="caption" color="text.secondary">
                  {isConnected ? "Live" : "Connecting..."}
                </Typography>
              </Box>
            )}
            {connectionError && (
              <Typography variant="caption" color="error">
                {connectionError}
              </Typography>
            )}
          </Box>
        )}
        <ChatView
          events={events}
          status={currentStatus}
          isConnected={isConnected}
          isCatchingUp={isCatchingUp}
        />
      </Box>
    </Box>
  );
}
