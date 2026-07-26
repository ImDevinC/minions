"use client";

import { useState } from "react";
import type { SubtaskThread, ChatMessage } from "@/types/minion";
import { ToolCallCard } from "./tool-call-card";
import { ThinkingBlock } from "./thinking-block";
import Accordion from "@mui/material/Accordion";
import AccordionSummary from "@mui/material/AccordionSummary";
import AccordionDetails from "@mui/material/AccordionDetails";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";

interface SubtaskBlockProps {
  subtask: SubtaskThread;
  expandedToolId: string | null;
  onToolToggle: (toolId: string | null) => void;
}

export function SubtaskBlock({
  subtask,
  expandedToolId,
  onToolToggle,
}: SubtaskBlockProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const agentLabel = subtask.agent ? ` (${subtask.agent})` : "";
  const headerText = `Subtask: ${subtask.description || "Task"}${agentLabel}`;

  return (
    <Accordion
      expanded={isExpanded}
      onChange={() => setIsExpanded(!isExpanded)}
      disableGutters
      sx={{
        backgroundColor: "rgba(0,0,0,0.15)",
        border: 1,
        borderColor: "divider",
      }}
    >
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Box sx={{ display: "flex", alignItems: "center", width: "100%", gap: 1 }}>
          <Typography
            variant="body2"
            color="cyan"
            noWrap
            sx={{ flex: 1, minWidth: 0 }}
          >
            {headerText}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {subtask.messages.length} msg{subtask.messages.length !== 1 ? "s" : ""}
          </Typography>
        </Box>
      </AccordionSummary>
      <AccordionDetails>
        <Box
          sx={{
            pl: 2,
            borderLeft: 2,
            borderColor: "primary.dark",
          }}
        >
          {subtask.messages.length === 0 ? (
            <Typography variant="body2" color="text.secondary" sx={{ fontStyle: "italic" }}>
              No messages yet
            </Typography>
          ) : (
            subtask.messages.map((message) => (
              <SubtaskMessageRow
                key={message.id}
                message={message}
                expandedToolId={expandedToolId}
                onToolToggle={onToolToggle}
              />
            ))
          )}
        </Box>
      </AccordionDetails>
    </Accordion>
  );
}

function SubtaskMessageRow({
  message,
  expandedToolId,
  onToolToggle,
}: {
  message: ChatMessage;
  expandedToolId: string | null;
  onToolToggle: (toolId: string | null) => void;
}) {
  const hasContent =
    message.text.trim() ||
    message.thinking ||
    message.tools.length > 0 ||
    message.subtasks.length > 0;

  if (!hasContent) {
    return null;
  }

  return (
    <Box sx={{ py: 1, borderBottom: 1, borderColor: "divider" }}>
      <Typography variant="caption" color="text.secondary" sx={{ display: "block",  mb: 1  }}>
        {new Date(message.timestamp).toLocaleTimeString()}
        {message.isStreaming && (
          <Box component="span" sx={{ color: "primary.main", ml: 1 }}>
            ● streaming
          </Box>
        )}
      </Typography>

      {message.thinking && <ThinkingBlock content={message.thinking} />}

      {message.text.trim() && (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word", mb: 1 }}
        >
          {message.text}
        </Typography>
      )}

      {message.tools.length > 0 && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
          {message.tools.map((tool) => (
            <ToolCallCard
              key={tool.id}
              tool={tool}
              isExpanded={expandedToolId === tool.id}
              onToggle={() => {
                onToolToggle(expandedToolId === tool.id ? null : tool.id);
              }}
            />
          ))}
        </Box>
      )}

      {message.subtasks.length > 0 && (
        <Box sx={{ mt: 1 }}>
          {message.subtasks.map((nestedSubtask) => (
            <SubtaskBlock
              key={nestedSubtask.sessionID}
              subtask={nestedSubtask}
              expandedToolId={expandedToolId}
              onToolToggle={onToolToggle}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}
