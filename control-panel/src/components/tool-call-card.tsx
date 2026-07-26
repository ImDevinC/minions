"use client";

import { useMemo, useState } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import type { ToolCall, ToolCallStatus } from "@/types/minion";
import Accordion from "@mui/material/Accordion";
import AccordionSummary from "@mui/material/AccordionSummary";
import AccordionDetails from "@mui/material/AccordionDetails";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import MenuBookIcon from "@mui/icons-material/MenuBook";
import EditIcon from "@mui/icons-material/Edit";
import SearchIcon from "@mui/icons-material/Search";
import TravelExploreIcon from "@mui/icons-material/TravelExplore";
import TerminalIcon from "@mui/icons-material/Terminal";
import SmartToyIcon from "@mui/icons-material/SmartToy";
import PublicIcon from "@mui/icons-material/Public";
import ListAltIcon from "@mui/icons-material/ListAlt";
import SchoolIcon from "@mui/icons-material/School";
import BuildIcon from "@mui/icons-material/Build";

const SIZE_THRESHOLD = 10 * 1024;
const PREVIEW_LENGTH = 500;

const TOOL_ICONS: Record<string, React.ReactNode> = {
  read: <MenuBookIcon fontSize="small" />,
  write: <EditIcon fontSize="small" />,
  edit: <EditIcon fontSize="small" />,
  glob: <SearchIcon fontSize="small" />,
  grep: <TravelExploreIcon fontSize="small" />,
  bash: <TerminalIcon fontSize="small" />,
  task: <SmartToyIcon fontSize="small" />,
  webfetch: <PublicIcon fontSize="small" />,
  todowrite: <ListAltIcon fontSize="small" />,
  skill: <SchoolIcon fontSize="small" />,
};

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
      if (tool.output) {
        const lines = tool.output.trim().split("\n").filter((l) => l.length > 0);
        return `Found ${lines.length} files`;
      }
      const pattern = (input.pattern || input.glob) as string | undefined;
      return pattern ? `Glob: ${pattern}` : "Finding files";
    }
    case "grep": {
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
        const truncated = command.length > 40 ? command.slice(0, 40) + "..." : command;
        return `Ran: ${truncated}`;
      }
      return "Ran command";
    }
    case "task":
    case "agent": {
      const agentType = (input.subagent_type || input.agent_type || input.type || input.agent) as
        | string
        | undefined;
      return agentType ? `Spawned ${agentType} agent` : "Spawned agent";
    }
    case "webfetch": {
      const url = input.url as string | undefined;
      if (url) {
        const truncated = url.length > 40 ? url.slice(0, 40) + "..." : url;
        return `Fetched ${truncated}`;
      }
      return "Fetched URL";
    }
    case "todowrite": {
      const todos = input.todos as unknown[] | undefined;
      if (Array.isArray(todos)) {
        return `Updated ${todos.length} todos`;
      }
      return "Updated todos";
    }
    case "skill": {
      const name = input.name as string | undefined;
      return name ? `Loaded skill: ${name}` : "Loaded skill";
    }
    default:
      return tool.tool.charAt(0).toUpperCase() + tool.tool.slice(1);
  }
}

function getStatusChipColor(status: ToolCallStatus): "default" | "primary" | "success" | "error" {
  switch (status) {
    case "pending":
      return "default";
    case "running":
      return "primary";
    case "completed":
      return "success";
    case "error":
      return "error";
    default:
      return "default";
  }
}

function TruncatedContent({
  content,
  label,
}: {
  content: string;
  label: string;
}) {
  const [showFull, setShowFull] = useState(false);
  const isLarge = content.length > SIZE_THRESHOLD;
  const displayContent = isLarge && !showFull ? content.slice(0, PREVIEW_LENGTH) + "..." : content;

  return (
    <Box>
      <Box
        component="pre"
        sx={{
          m: 0,
          p: 1,
          borderRadius: 1,
          backgroundColor: "rgba(0,0,0,0.2)",
          overflowX: "auto",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          fontSize: "0.75rem",
          fontFamily: "monospace",
        }}
      >
        {displayContent}
      </Box>
      {isLarge && (
        <Button size="small" onClick={() => setShowFull(!showFull)} sx={{ mt: 1 }}>
          {showFull ? `Hide full ${label}` : `Show full ${label}`}
        </Button>
      )}
    </Box>
  );
}

function TruncatedSyntaxHighlighter({ content, label }: { content: string; label: string }) {
  const [showFull, setShowFull] = useState(false);
  const isLarge = content.length > SIZE_THRESHOLD;
  const displayContent = isLarge && !showFull ? content.slice(0, PREVIEW_LENGTH) + "..." : content;

  return (
    <Box>
      <Box sx={{ borderRadius: 1, overflow: "hidden", border: 1, borderColor: "divider" }}>
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
      </Box>
      {isLarge && (
        <Button size="small" onClick={() => setShowFull(!showFull)} sx={{ mt: 1 }}>
          {showFull ? `Hide full ${label}` : `Show full ${label}`}
        </Button>
      )}
    </Box>
  );
}

interface ToolCallCardProps {
  tool: ToolCall;
  isExpanded: boolean;
  onToggle: () => void;
}

export function ToolCallCard({ tool, isExpanded, onToggle }: ToolCallCardProps) {
  const inputJson = useMemo(() => {
    try {
      return JSON.stringify(tool.input, null, 2);
    } catch {
      return String(tool.input);
    }
  }, [tool.input]);

  const summary = useMemo(() => getToolSummary(tool), [tool]);
  const icon = TOOL_ICONS[tool.tool.toLowerCase()] || <BuildIcon fontSize="small" />;
  const statusColor = getStatusChipColor(tool.status);

  return (
    <Accordion
      expanded={isExpanded}
      onChange={() => onToggle()}
      disableGutters
      sx={{ backgroundColor: "rgba(0,0,0,0.2)", border: 1, borderColor: "divider" }}
    >
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, width: "100%", minWidth: 0 }}>
          {icon}
          <Typography
            variant="body2"
            sx={{ fontWeight: 500, display: { xs: "none", sm: "inline" }, flexShrink: 0 }}
          >
            {tool.tool}
          </Typography>
          <Chip size="small" color={statusColor} label={tool.status} sx={{ flexShrink: 0 }} />
          <Typography
            variant="body2"
            color="text.secondary"
            noWrap
            sx={{ flex: 1, minWidth: 0 }}
          >
            {summary}
          </Typography>
        </Box>
      </AccordionSummary>
      <AccordionDetails>
        <Box sx={{ mb: 2 }}>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block" }} gutterBottom>
            Input
          </Typography>
          <TruncatedSyntaxHighlighter content={inputJson} label="input" />
        </Box>
        {tool.output && (
          <Box sx={{ mb: 2 }}>
            <Typography variant="caption" color="text.secondary" sx={{ display: "block" }} gutterBottom>
              Output
            </Typography>
            <TruncatedContent
              content={tool.output}
              label="output"
            />
          </Box>
        )}
        {tool.error && (
          <Box>
            <Typography variant="caption" color="error" sx={{ display: "block" }} gutterBottom>
              Error
            </Typography>
            <TruncatedContent
              content={tool.error}
              label="error"
            />
          </Box>
        )}
      </AccordionDetails>
    </Accordion>
  );
}
