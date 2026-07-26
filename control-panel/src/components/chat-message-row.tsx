"use client";

import { memo, useMemo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import type { ChatMessage, TextPart } from "@/types/minion";
import { ThinkingBlock } from "./thinking-block";
import { ToolCallCard } from "./tool-call-card";
import { SubtaskBlock } from "./subtask-block";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

interface ChatMessageRowProps {
  message: ChatMessage;
  expandedToolId: string | null;
  onToolToggle: (toolId: string | null) => void;
}

export const ChatMessageRow = memo(function ChatMessageRow({
  message,
  expandedToolId,
  onToolToggle,
}: ChatMessageRowProps) {
  const hasContent =
    message.text.trim() ||
    message.thinking ||
    message.tools.length > 0 ||
    message.subtasks.length > 0;

  if (!hasContent) {
    return null;
  }

  return (
    <Box sx={{ py: 1.5, px: 2, borderBottom: 1, borderColor: "divider" }}>
      <Typography variant="caption" color="text.secondary" sx={{ display: "block",  mb: 1  }}>
        {new Date(message.timestamp).toLocaleTimeString()}
        {message.isStreaming && (
          <Box component="span" sx={{ color: "primary.main", ml: 1 }}>
            ● streaming
          </Box>
        )}
      </Typography>

      {message.thinking && <ThinkingBlock content={message.thinking} />}

      {message.textParts.length > 0 && (
        <Box>
          {message.textParts.map((part, idx) => (
            <MemoizedMarkdownPart
              key={part.id}
              partId={part.id}
              text={part.text}
              isLast={idx === message.textParts.length - 1}
            />
          ))}
        </Box>
      )}

      {message.tools.length > 0 && (
        <Box sx={{ mt: 1, display: "flex", flexDirection: "column", gap: 0.5 }}>
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
          {message.subtasks.map((subtask) => (
            <SubtaskBlock
              key={subtask.sessionID}
              subtask={subtask}
              expandedToolId={expandedToolId}
              onToolToggle={onToolToggle}
            />
          ))}
        </Box>
      )}
    </Box>
  );
});

interface MarkdownPartProps {
  partId: string;
  text: string;
  isLast: boolean;
}

const MemoizedMarkdownPart = memo(function MarkdownPart({ text, isLast }: MarkdownPartProps) {
  const content = useMemo(
    () => (
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    ),
    [text]
  );

  return <Box sx={isLast ? {} : { mb: 2 }}>{content}</Box>;
});

const markdownComponents = {
  code({
    className,
    children,
  }: {
    node?: unknown;
    className?: string;
    children?: React.ReactNode;
  }) {
    const match = /language-(\w+)/.exec(className || "");
    const language = match ? match[1] : "";
    const codeString = String(children).replace(/\n$/, "");
    const isInline = !match && !codeString.includes("\n");

    if (isInline) {
      return (
        <Box
          component="code"
          sx={{
            backgroundColor: "rgba(255,255,255,0.1)",
            px: 0.5,
            py: 0.25,
            borderRadius: 0.5,
            fontSize: "0.875rem",
            fontFamily: "monospace",
          }}
        >
          {children}
        </Box>
      );
    }

    return (
      <Box sx={{ my: 1.5, borderRadius: 1, overflow: "hidden", border: 1, borderColor: "divider" }}>
        {language && (
          <Box sx={{ backgroundColor: "rgba(255,255,255,0.05)", px: 1.5, py: 0.5 }}>
            <Typography variant="caption" color="text.secondary">
              {language}
            </Typography>
          </Box>
        )}
        <Box sx={{ overflowX: "auto" }}>
          <SyntaxHighlighter
            language={language || "text"}
            style={oneDark}
            customStyle={{
              margin: 0,
              padding: "1rem",
              background: "#1a1a2e",
              fontSize: "0.875rem",
              whiteSpace: "pre",
              overflowX: "auto",
            }}
            wrapLines={false}
            wrapLongLines={false}
          >
            {codeString}
          </SyntaxHighlighter>
        </Box>
      </Box>
    );
  },
  p({ children }: { children?: React.ReactNode }) {
    return (
      <Typography variant="body1" sx={{ mb: 1.5, "&:last-child": { mb: 0 } }}>
        {children}
      </Typography>
    );
  },
  a({ href, children }: { href?: string; children?: React.ReactNode }) {
    return (
      <Typography
        component="a"
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        color="primary.main"
        sx={{ textDecoration: "underline" }}
      >
        {children}
      </Typography>
    );
  },
  ul({ children }: { children?: React.ReactNode }) {
    return (
      <Box component="ul" sx={{ pl: 3, mb: 1.5, listStyleType: "disc" }}>
        {children}
      </Box>
    );
  },
  ol({ children }: { children?: React.ReactNode }) {
    return (
      <Box component="ol" sx={{ pl: 3, mb: 1.5, listStyleType: "decimal" }}>
        {children}
      </Box>
    );
  },
  li({ children }: { children?: React.ReactNode }) {
    return <Box component="li">{children}</Box>;
  },
  h1({ children }: { children?: React.ReactNode }) {
    return (
      <Typography variant="h4" component="h1" sx={{ mb: 2, mt: 3 }}>
        {children}
      </Typography>
    );
  },
  h2({ children }: { children?: React.ReactNode }) {
    return (
      <Typography variant="h5" component="h2" sx={{ mb: 1.5, mt: 2.5 }}>
        {children}
      </Typography>
    );
  },
  h3({ children }: { children?: React.ReactNode }) {
    return (
      <Typography variant="h6" component="h3" sx={{ mb: 1.5, mt: 2 }}>
        {children}
      </Typography>
    );
  },
  blockquote({ children }: { children?: React.ReactNode }) {
    return (
      <Box
        component="blockquote"
        sx={{
          borderLeft: 4,
          borderColor: "divider",
          pl: 2,
          my: 1.5,
          color: "text.secondary",
          fontStyle: "italic",
        }}
      >
        {children}
      </Box>
    );
  },
  hr() {
    return <Box component="hr" sx={{ borderColor: "divider", my: 2 }} />;
  },
  table({ children }: { children?: React.ReactNode }) {
    return (
      <Box sx={{ overflowX: "auto", my: 1.5 }}>
        <Box component="table" sx={{ minWidth: "100%", borderCollapse: "collapse", fontSize: "0.875rem" }}>
          {children}
        </Box>
      </Box>
    );
  },
  thead({ children }: { children?: React.ReactNode }) {
    return (
      <Box component="thead" sx={{ backgroundColor: "rgba(255,255,255,0.05)" }}>
        {children}
      </Box>
    );
  },
  th({ children }: { children?: React.ReactNode }) {
    return (
      <Box component="th" sx={{ px: 1.5, py: 1, textAlign: "left", borderBottom: 1, borderColor: "divider" }}>
        {children}
      </Box>
    );
  },
  td({ children }: { children?: React.ReactNode }) {
    return (
      <Box component="td" sx={{ px: 1.5, py: 1, borderBottom: 1, borderColor: "divider" }}>
        {children}
      </Box>
    );
  },
  pre({ children }: { children?: React.ReactNode }) {
    return <>{children}</>;
  },
};
