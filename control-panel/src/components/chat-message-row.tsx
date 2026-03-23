"use client";

import { useMemo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import type { ChatMessage } from "@/types/minion";

interface ChatMessageRowProps {
  message: ChatMessage;
}

/**
 * ChatMessageRow renders aggregated chat message content as markdown.
 *
 * Features:
 * - Text renders via react-markdown with remark-gfm for GitHub-flavored markdown
 * - Code blocks render with react-syntax-highlighter and oneDark theme
 * - Code blocks scroll horizontally (no forced line wrap)
 * - Empty or null content does not render
 */
export function ChatMessageRow({ message }: ChatMessageRowProps) {
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

      {/* Thinking block placeholder - will be replaced in component-3 */}
      {message.thinking && (
        <div className="text-sm text-gray-500 italic mb-2 border-l-2 border-gray-700 pl-3">
          [Thinking: {message.thinking.slice(0, 100)}...]
        </div>
      )}

      {/* Markdown text content */}
      {message.text.trim() && (
        <MarkdownContent text={message.text} />
      )}

      {/* Tool calls placeholder - will be replaced in component-4 */}
      {message.tools.length > 0 && (
        <div className="mt-2 text-xs text-purple-400">
          {message.tools.length} tool call(s)
        </div>
      )}

      {/* Subtasks placeholder - will be replaced in component-6 */}
      {message.subtasks.length > 0 && (
        <div className="mt-2 text-xs text-cyan-400">
          {message.subtasks.length} subtask(s)
        </div>
      )}
    </div>
  );
}

/**
 * MarkdownContent renders text as GitHub-flavored markdown with syntax highlighting.
 * Memoized to avoid re-parsing on every render.
 */
function MarkdownContent({ text }: { text: string }) {
  // Memoize markdown rendering - text is the only dependency
  const content = useMemo(() => (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        // Custom code block renderer with syntax highlighting
        code({ node, className, children, ...props }) {
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
        p({ children }) {
          return <p className="text-gray-200 mb-3 last:mb-0 leading-relaxed">{children}</p>;
        },
        a({ href, children }) {
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
        ul({ children }) {
          return <ul className="list-disc list-inside mb-3 text-gray-200 space-y-1">{children}</ul>;
        },
        ol({ children }) {
          return <ol className="list-decimal list-inside mb-3 text-gray-200 space-y-1">{children}</ol>;
        },
        li({ children }) {
          return <li className="text-gray-200">{children}</li>;
        },
        h1({ children }) {
          return <h1 className="text-xl font-bold text-white mb-3 mt-4 first:mt-0">{children}</h1>;
        },
        h2({ children }) {
          return <h2 className="text-lg font-bold text-white mb-2 mt-3 first:mt-0">{children}</h2>;
        },
        h3({ children }) {
          return <h3 className="text-base font-semibold text-white mb-2 mt-3 first:mt-0">{children}</h3>;
        },
        blockquote({ children }) {
          return (
            <blockquote className="border-l-4 border-gray-600 pl-4 my-3 text-gray-400 italic">
              {children}
            </blockquote>
          );
        },
        hr() {
          return <hr className="border-gray-700 my-4" />;
        },
        table({ children }) {
          return (
            <div className="overflow-x-auto my-3">
              <table className="min-w-full border border-gray-700 text-sm">{children}</table>
            </div>
          );
        },
        thead({ children }) {
          return <thead className="bg-gray-800">{children}</thead>;
        },
        th({ children }) {
          return <th className="px-3 py-2 text-left text-gray-300 font-medium border-b border-gray-700">{children}</th>;
        },
        td({ children }) {
          return <td className="px-3 py-2 text-gray-200 border-b border-gray-800">{children}</td>;
        },
        // Handle pre tags (wrapper for code blocks)
        pre({ children }) {
          // Just pass through - the code component handles styling
          return <>{children}</>;
        },
      }}
    >
      {text}
    </ReactMarkdown>
  ), [text]);

  return <div className="prose prose-invert max-w-none">{content}</div>;
}
