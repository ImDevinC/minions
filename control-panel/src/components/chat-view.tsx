"use client";

import { useRef, useState, useCallback, useMemo, useEffect } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { MinionEvent, ChatMessage, SystemMessage, MinionStatus } from "@/types/minion";
import {
  aggregateEvents,
  createDeltaState,
  type DeltaState,
} from "@/lib/event-aggregation";
import { ChatMessageRow } from "./chat-message-row";
import { SystemMessageRow } from "./system-message-row";
import Paper from "@mui/material/Paper";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Fab from "@mui/material/Fab";
import Skeleton from "@mui/material/Skeleton";
import LinearProgress from "@mui/material/LinearProgress";
import ArrowDownwardIcon from "@mui/icons-material/ArrowDownward";
import HourglassEmptyIcon from "@mui/icons-material/HourglassEmpty";

type SessionState = "busy" | "idle" | "retry" | "completed" | "failed" | "terminated";

export function deriveSessionState(events: MinionEvent[]): SessionState | null {
  if (events.length === 0) return null;
  const recentEvents = events.slice(-10);

  for (let i = recentEvents.length - 1; i >= 0; i--) {
    const event = recentEvents[i];
    const content = event.content;

    if (
      event.event_type === "retry" ||
      (typeof content.type === "string" && content.type === "retry")
    ) {
      return "retry";
    }

    if (
      content.status !== null &&
      typeof content.status === "object" &&
      !Array.isArray(content.status)
    ) {
      const status = content.status as Record<string, unknown>;
      if (status.thinking === true) {
        return "busy";
      }
    }

    if (event.event_type === "message.updated") {
      const status = content.status as string | undefined;
      if (status === "streaming" || status === "pending") {
        return "busy";
      }
    }

    if (event.event_type === "part.delta" || event.event_type === "text.delta") {
      return "busy";
    }
  }

  return "idle";
}

function SessionStatusBar({ state }: { state: SessionState }) {
  const config = {
    busy: { color: "primary" as const, text: "Agent is thinking..." },
    idle: { color: "default" as const, text: "Idle" },
    retry: { color: "warning" as const, text: "Retrying..." },
    completed: { color: "success" as const, text: "Completed" },
    failed: { color: "error" as const, text: "Failed" },
    terminated: { color: "warning" as const, text: "Terminated" },
  }[state];

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1,
        px: 2,
        py: 0.75,
        borderBottom: 1,
        borderColor: "divider",
        backgroundColor: "rgba(255,255,255,0.03)",
      }}
    >
      <Box
        sx={{
          width: 8,
          height: 8,
          borderRadius: "50%",
          backgroundColor: config.color === "primary" ? "primary.main" : "text.secondary",
          animation: config.color === "primary" ? "pulse 1.5s infinite" : "none",
        }}
      />
      <Typography variant="caption" color="text.secondary">
        {config.text}
      </Typography>
    </Box>
  );
}

type RenderItem =
  | { type: "chat"; message: ChatMessage }
  | { type: "system"; message: SystemMessage };

interface ChatViewProps {
  events: MinionEvent[];
  status?: MinionStatus;
  isConnected?: boolean;
  isLoading?: boolean;
  isCatchingUp?: boolean;
}

export function ChatView({
  events,
  status,
  isConnected,
  isLoading = false,
  isCatchingUp = false,
}: ChatViewProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const [expandedToolId, setExpandedToolId] = useState<string | null>(null);
  const deltaStateRef = useRef<DeltaState>(createDeltaState());

  const { messages, systemMessages } = useMemo(() => {
    return aggregateEvents(events, deltaStateRef.current);
  }, [events]);

  const renderItems: RenderItem[] = useMemo(() => {
    const items: RenderItem[] = [];
    for (const msg of messages) {
      items.push({ type: "chat", message: msg });
    }
    for (const msg of systemMessages) {
      items.push({ type: "system", message: msg });
    }
    items.sort((a, b) => {
      const timeA = new Date(a.message.timestamp).getTime();
      const timeB = new Date(b.message.timestamp).getTime();
      return timeA - timeB;
    });
    return items;
  }, [messages, systemMessages]);

  const virtualizer = useVirtualizer({
    count: renderItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const item = renderItems[index];
      if (item.type === "system") {
        return 32;
      }
      const msg = item.message;
      const textLength = msg.text.length;
      const hasTools = msg.tools.length > 0;
      const hasSubtasks = msg.subtasks.length > 0;
      return 80 + Math.min(textLength / 10, 200) + (hasTools ? 24 : 0) + (hasSubtasks ? 24 : 0);
    },
    overscan: 20,
  });

  useEffect(() => {
    if (autoScroll && renderItems.length > 0) {
      virtualizer.scrollToIndex(renderItems.length - 1, { align: "end" });
    }
  }, [renderItems.length, autoScroll, virtualizer]);

  const handleScroll = useCallback(() => {
    if (!parentRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = parentRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 100;
    setAutoScroll(isAtBottom);
  }, []);

  const isTerminalStatus =
    status === "completed" || status === "failed" || status === "terminated";

  const sessionState = useMemo(() => {
    if (status === "completed") return "completed";
    if (status === "failed") return "failed";
    if (status === "terminated") return "terminated";
    if (status !== "running") return null;
    return deriveSessionState(events);
  }, [events, status]);

  if (isLoading) {
    return (
      <Paper
        sx={{
          p: 2,
          height: { xs: 400, md: 500 },
          display: "flex",
          flexDirection: "column",
          gap: 2,
          overflow: "hidden",
        }}
      >
        {[1, 2, 3].map((i) => (
          <Box key={i}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
              <Skeleton width={60} height={16} />
              <Skeleton variant="circular" width={20} height={20} />
            </Box>
            <Skeleton width="75%" height={20} />
            <Skeleton width="50%" height={20} />
          </Box>
        ))}
      </Paper>
    );
  }

  if (renderItems.length === 0) {
    if (status === "running" || status === "pending") {
      return (
        <Paper
          sx={{
            height: 256,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexDirection: "column",
            gap: 2,
          }}
        >
          <HourglassEmptyIcon sx={{ fontSize: 48, color: "text.disabled" }} />
          <Typography variant="body1" color="text.secondary">
            Waiting for agent to start...
          </Typography>
        </Paper>
      );
    }

    if (isTerminalStatus && status === "completed") {
      return (
        <Paper
          sx={{
            height: 256,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <Typography variant="body1" color="text.secondary">
            No output produced
          </Typography>
        </Paper>
      );
    }

    return (
      <Paper
        sx={{
          height: 256,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography variant="body1" color="text.secondary">
          No events yet
        </Typography>
      </Paper>
    );
  }

  return (
    <Box sx={{ position: "relative" }}>
      {isCatchingUp && (
        <Box
          sx={{
            position: "absolute",
            top: 8,
            left: "50%",
            transform: "translateX(-50%)",
            zIndex: 2,
            px: 2,
            py: 0.5,
            borderRadius: 4,
            backgroundColor: "warning.main",
            color: "warning.contrastText",
            display: "flex",
            alignItems: "center",
            gap: 1,
            boxShadow: 3,
          }}
        >
          <Box
            sx={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              backgroundColor: "currentColor",
              animation: "pulse 1.5s infinite",
            }}
          />
          <Typography variant="caption">Catching up...</Typography>
        </Box>
      )}

      {!autoScroll && (
        <Fab
          size="small"
          color="primary"
          sx={{
            position: "absolute",
            bottom: 16,
            right: 16,
            zIndex: 2,
          }}
          onClick={() => {
            setAutoScroll(true);
            virtualizer.scrollToIndex(renderItems.length - 1, { align: "end" });
          }}
        >
          <ArrowDownwardIcon />
        </Fab>
      )}

      <Paper
        elevation={2}
        sx={{
          overflow: "hidden",
          border: 1,
          borderColor: "divider",
        }}
      >
        {sessionState && <SessionStatusBar state={sessionState} />}
        <Box
          ref={parentRef}
          onScroll={handleScroll}
          sx={{
            height: { xs: 400, md: 500 },
            overflow: "auto",
            position: "relative",
          }}
        >
          <Box sx={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}>
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const item = renderItems[virtualRow.index];
              return (
                <Box
                  key={virtualRow.key}
                  sx={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                  data-index={virtualRow.index}
                  ref={virtualizer.measureElement}
                >
                  {item.type === "chat" ? (
                    <ChatMessageRow
                      message={item.message}
                      expandedToolId={
                        expandedToolId?.startsWith(`${item.message.id}:`)
                          ? expandedToolId.slice(item.message.id.length + 1)
                          : null
                      }
                      onToolToggle={(toolId) => {
                        setExpandedToolId(
                          toolId ? `${item.message.id}:${toolId}` : null
                        );
                      }}
                    />
                  ) : (
                    <SystemMessageRow message={item.message} />
                  )}
                </Box>
              );
            })}
          </Box>
        </Box>
      </Paper>
    </Box>
  );
}
