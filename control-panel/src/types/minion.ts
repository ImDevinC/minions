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
