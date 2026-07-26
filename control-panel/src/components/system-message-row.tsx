"use client";

import type { SystemMessage } from "@/types/minion";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import WarningIcon from "@mui/icons-material/Warning";
import RefreshIcon from "@mui/icons-material/Refresh";
import SmartToyIcon from "@mui/icons-material/SmartToy";

function formatTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function getRetryAttempt(content: string | Record<string, unknown>): number {
  if (typeof content === "object" && content !== null) {
    const attempt = content.attempt ?? content.retry_count ?? content.retryCount;
    if (typeof attempt === "number") return attempt;
    const metadata = content.metadata as Record<string, unknown> | undefined;
    if (metadata) {
      const metaAttempt = metadata.attempt ?? metadata.retry_count;
      if (typeof metaAttempt === "number") return metaAttempt;
    }
  }
  return 0;
}

function getAgentName(content: string | Record<string, unknown>): string {
  if (typeof content === "string") return content;
  if (typeof content === "object" && content !== null) {
    const agent = content.agent ?? content.name ?? content.type;
    if (typeof agent === "string") return agent;
  }
  return "unknown";
}

function getErrorMessage(content: string | Record<string, unknown>): string {
  if (typeof content === "string") return content;
  if (typeof content === "object" && content !== null) {
    const error = content.error ?? content.message ?? content.reason;
    if (typeof error === "string") return error;
    return JSON.stringify(content, null, 2);
  }
  return "Unknown error";
}

interface SystemMessageRowProps {
  message: SystemMessage;
}

export function SystemMessageRow({ message }: SystemMessageRowProps) {
  const time = formatTime(message.timestamp);

  if (message.type === "agent") {
    const agentName = getAgentName(message.content);
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          py: 0.5,
          px: 2,
          backgroundColor: "rgba(255,255,255,0.02)",
        }}
      >
        <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>
          {time}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          <SmartToyIcon fontSize="inherit" sx={{ verticalAlign: "middle", mr: 0.5 }} />
          Switched to <strong>{agentName}</strong> agent
        </Typography>
      </Box>
    );
  }

  if (message.type === "session.error" || message.type === "error") {
    const errorMsg = getErrorMessage(message.content);
    return (
      <Alert severity="error" sx={{ mx: 1, my: 0.5 }}>
        <Box sx={{ display: "flex", gap: 1 }}>
          <Typography variant="caption" color="error" sx={{ flexShrink: 0 }}>
            {time}
          </Typography>
          <Typography
            variant="caption"
            component="span"
            sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}
          >
            {errorMsg}
          </Typography>
        </Box>
      </Alert>
    );
  }

  if (message.type === "retry") {
    const attempt = getRetryAttempt(message.content);
    const attemptText = attempt > 0 ? `, attempt ${attempt}` : "";
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          py: 0.5,
          px: 2,
          backgroundColor: "rgba(250, 204, 21, 0.08)",
          borderLeft: 2,
          borderColor: "warning.main",
        }}
      >
        <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>
          {time}
        </Typography>
        <Typography variant="caption" color="warning.main">
          <RefreshIcon fontSize="inherit" sx={{ verticalAlign: "middle", mr: 0.5 }} />
          Retrying{attemptText}
        </Typography>
      </Box>
    );
  }

  const displayContent =
    typeof message.content === "string"
      ? message.content
      : JSON.stringify(message.content);

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1,
        py: 0.5,
        px: 2,
        backgroundColor: "rgba(255,255,255,0.02)",
      }}
    >
      <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>
        {time}
      </Typography>
      <Typography variant="caption" color="text.secondary" noWrap>
        {displayContent}
      </Typography>
    </Box>
  );
}
