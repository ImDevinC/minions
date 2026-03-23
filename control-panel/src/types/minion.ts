// Minion types matching orchestrator API responses

export type MinionStatus =
  | "pending"
  | "awaiting_clarification"
  | "running"
  | "completed"
  | "failed"
  | "terminated";

// Response from GET /api/minions (list endpoint)
export interface MinionSummary {
  id: string;
  status: MinionStatus;
  repo: string;
  task: string;
  model: string;
  pr_url?: string;
  error?: string;
  cost_usd: number;
  created_at: string;
}

// Response from GET /api/minions/:id (detail endpoint)
export interface MinionDetail {
  id: string;
  user_id: string;
  repo: string;
  task: string;
  model: string;
  status: MinionStatus;
  clarification_question?: string;
  clarification_answer?: string;
  clarification_message_id?: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  pr_url?: string;
  error?: string;
  session_id?: string;
  pod_name?: string;
  discord_message_id?: string;
  discord_channel_id?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  last_activity_at: string;
  events: MinionEvent[];
}

export interface MinionEvent {
  id: string;
  timestamp: string;
  event_type: string;
  content: Record<string, unknown>;
}

// Chat view types for aggregated message rendering

export type ToolCallStatus = "pending" | "running" | "completed" | "error";

/**
 * A tool invocation within a ChatMessage.
 * Rendered as a compact expandable card.
 */
export interface ToolCall {
  id: string;
  tool: string;
  status: ToolCallStatus;
  title?: string;
  /** Human-readable summary (e.g., "Read src/foo.ts") */
  summary: string;
  input: Record<string, unknown>;
  output?: string;
  error?: string;
}

/**
 * A nested conversation thread from a subtask (spawned subagent).
 * The sessionID links events from the child session to this thread.
 */
export interface SubtaskThread {
  /** Session ID of the subtask (matches callID of the task tool) */
  sessionID: string;
  /** Human-readable description of the subtask */
  description: string;
  /** Agent type (e.g., "task", "explore", "code-reviewer") */
  agent?: string;
  /** Nested messages from the subtask session */
  messages: ChatMessage[];
}

/**
 * An aggregated chat message from grouped events.
 * Events with the same messageID are combined into one ChatMessage.
 */
export interface ChatMessage {
  id: string;
  timestamp: string;
  /** Accumulated reasoning/thinking text (collapsed by default) */
  thinking?: string;
  /** Accumulated text content rendered as markdown */
  text: string;
  /** Tool calls in chronological order */
  tools: ToolCall[];
  /** Nested subtask threads (spawned subagents) */
  subtasks: SubtaskThread[];
  /** True while message is still receiving streaming events */
  isStreaming: boolean;
}

/**
 * A system message for events without a messageID.
 * Rendered as subtle banners outside the main conversation flow.
 */
export interface SystemMessage {
  id: string;
  timestamp: string;
  /** System message type (e.g., "agent", "session.error", "retry") */
  type: string;
  /** Display content or raw event data */
  content: string | Record<string, unknown>;
}

// Response from GET /api/stats
export interface Stats {
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  by_model: ModelStats[];
}

export interface ModelStats {
  model: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  count: number;
}
