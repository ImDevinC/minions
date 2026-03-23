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

interface ChatMessageRowProps {
  message: ChatMessage;
  /** ID of the currently expanded tool card (for accordion behavior) */
  expandedToolId: string | null;
  /** Callback when a tool card is toggled */
  onToolToggle: (toolId: string | null) => void;
}

/**
 * ChatMessageRow renders aggregated chat message content as markdown.
 *
 * Features:
 * - Text renders via react-markdown with remark-gfm for GitHub-flavored markdown
 * - Code blocks render with react-syntax-highlighter and oneDark theme
 * - Code blocks scroll horizontally (no forced line wrap)
 * - Empty or null content does not render
 * - Text parts are rendered individually for efficient memoization
 */
export const ChatMessageRow = memo(function ChatMessageRow({ message, expandedToolId, onToolToggle }: ChatMessageRowProps) {
  // Skip rendering if no meaningful content
  const hasContent = message.text.trim() || message.thinking || message.tools.length > 0 || message.subtasks.length > 0;
  
  if (!hasContent) {
    return null;
  }

  return (
    <div className="py-3 px-4 border-b border-gray-800">
      {/* Timestamp and streaming indicator */}
      <div className="text-xs text-gray-500 mb-2">
        {new Date(message.timestamp).toLocaleTimeString()}
        {message.isStreaming && (
          <span className="ml-2 text-blue-400">
            <span className="inline-block w-1.5 h-1.5 bg-blue-400 rounded-full animate-pulse mr-1" />
            streaming
          </span>
        )}
      </div>

      {/* Collapsible thinking/reasoning block */}
      {message.thinking && (
        <ThinkingBlock content={message.thinking} />
      )}

      {/* Markdown text content - rendered per-part for memoization */}
      {message.textParts.length > 0 && (
        <div className="prose prose-invert max-w-none">
          {message.textParts.map((part, idx) => (
            <MemoizedMarkdownPart
              key={part.id}
              partId={part.id}
              text={part.text}
              isLast={idx === message.textParts.length - 1}
            />
          ))}
        </div>
      )}

      {/* Tool calls rendered as expandable cards */}
      {message.tools.length > 0 && (
        <div className="mt-2 space-y-1">
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
        </div>
      )}

      {/* Subtask threads - nested conversation from spawned subagents */}
      {message.subtasks.length > 0 && (
        <div className="mt-2">
          {message.subtasks.map((subtask) => (
            <SubtaskBlock
              key={subtask.sessionID}
              subtask={subtask}
              expandedToolId={expandedToolId}
              onToolToggle={onToolToggle}
            />
          ))}
        </div>
      )}
    </div>
  );
});

interface MarkdownPartProps {
  partId: string;
  text: string;
  isLast: boolean;
}

/**
 * MemoizedMarkdownPart renders a single text part as markdown.
 * 
 * Memoized by (partId + text) to avoid re-parsing unchanged parts.
 * During streaming, only the last part (actively receiving deltas) re-renders;
 * previous parts remain cached.
 */
const MemoizedMarkdownPart = memo(function MarkdownPart({ text, isLast }: MarkdownPartProps) {
  // Memoize the rendered content
  const content = useMemo(() => (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={markdownComponents}
    >
      {text}
    </ReactMarkdown>
  ), [text]);

  // Add spacing between parts (except last)
  return <div className={isLast ? "" : "mb-4"}>{content}</div>;
}, (prev, next) => {
  // Only re-render if partId or text changed
  return prev.partId === next.partId && prev.text === next.text && prev.isLast === next.isLast;
});

/**
 * Shared markdown component config to avoid recreating on each render.
 * Extracted to module level for stable reference.
 */
const markdownComponents = {
  // Custom code block renderer with syntax highlighting
  code({ node, className, children, ...props }: { node?: unknown; className?: string; children?: React.ReactNode }) {
    const match = /language-(\w+)/.exec(className || "");
    const language = match ? match[1] : "";
    const codeString = String(children).replace(/\n$/, "");
    
    // Inline code (no language, short content)
    const isInline = !match && !codeString.includes("\n");
    
    if (isInline) {
      return (
        <code
          className="bg-gray-800 text-gray-200 px-1.5 py-0.5 rounded text-sm font-mono"
          {...props}
        >
          {children}
        </code>
      );
    }

    // Fenced code block with syntax highlighting
    return (
      <div className="my-3 rounded-lg overflow-hidden border border-gray-700">
        {language && (
          <div className="bg-gray-800 px-3 py-1 text-xs text-gray-400 border-b border-gray-700">
            {language}
          </div>
        )}
        <div className="overflow-x-auto">
          <SyntaxHighlighter
            language={language || "text"}
            style={oneDark}
            customStyle={{
              margin: 0,
              padding: "1rem",
              background: "#1a1a2e",
              fontSize: "0.875rem",
              // Enable horizontal scrolling, disable line wrap
              whiteSpace: "pre",
              overflowX: "auto",
            }}
            // Do NOT use wrapLines or wrapLongLines - we want horizontal scroll
            wrapLines={false}
            wrapLongLines={false}
          >
            {codeString}
          </SyntaxHighlighter>
        </div>
      </div>
    );
  },
  // Style other markdown elements
  p({ children }: { children?: React.ReactNode }) {
    return <p className="text-gray-200 mb-3 last:mb-0 leading-relaxed">{children}</p>;
  },
  a({ href, children }: { href?: string; children?: React.ReactNode }) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-blue-400 hover:text-blue-300 underline"
      >
        {children}
      </a>
    );
  },
  ul({ children }: { children?: React.ReactNode }) {
    return <ul className="list-disc list-inside mb-3 text-gray-200 space-y-1">{children}</ul>;
  },
  ol({ children }: { children?: React.ReactNode }) {
    return <ol className="list-decimal list-inside mb-3 text-gray-200 space-y-1">{children}</ol>;
  },
  li({ children }: { children?: React.ReactNode }) {
    return <li className="text-gray-200">{children}</li>;
  },
  h1({ children }: { children?: React.ReactNode }) {
    return <h1 className="text-xl font-bold text-white mb-3 mt-4 first:mt-0">{children}</h1>;
  },
  h2({ children }: { children?: React.ReactNode }) {
    return <h2 className="text-lg font-bold text-white mb-2 mt-3 first:mt-0">{children}</h2>;
  },
  h3({ children }: { children?: React.ReactNode }) {
    return <h3 className="text-base font-semibold text-white mb-2 mt-3 first:mt-0">{children}</h3>;
  },
  blockquote({ children }: { children?: React.ReactNode }) {
    return (
      <blockquote className="border-l-4 border-gray-600 pl-4 my-3 text-gray-400 italic">
        {children}
      </blockquote>
    );
  },
  hr() {
    return <hr className="border-gray-700 my-4" />;
  },
  table({ children }: { children?: React.ReactNode }) {
    return (
      <div className="overflow-x-auto my-3">
        <table className="min-w-full border border-gray-700 text-sm">{children}</table>
      </div>
    );
  },
  thead({ children }: { children?: React.ReactNode }) {
    return <thead className="bg-gray-800">{children}</thead>;
  },
  th({ children }: { children?: React.ReactNode }) {
    return <th className="px-3 py-2 text-left text-gray-300 font-medium border-b border-gray-700">{children}</th>;
  },
  td({ children }: { children?: React.ReactNode }) {
    return <td className="px-3 py-2 text-gray-200 border-b border-gray-800">{children}</td>;
  },
  // Handle pre tags (wrapper for code blocks)
  pre({ children }: { children?: React.ReactNode }) {
    // Just pass through - the code component handles styling
    return <>{children}</>;
  },
};
