-- Migration: 002_create_minions_table.sql
-- Creates the minions table for tracking task lifecycle

-- Status enum as a domain constraint (CHECK constraint vs. ENUM type)
-- Using CHECK for easier future additions without migration
CREATE TABLE IF NOT EXISTS minions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Task definition
    repo TEXT NOT NULL,
    task TEXT NOT NULL,
    model TEXT NOT NULL,
    
    -- Lifecycle state
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'awaiting_clarification', 'running', 'completed', 'failed', 'terminated')
    ),
    
    -- Clarification flow
    clarification_question TEXT,
    clarification_answer TEXT,
    clarification_message_id TEXT,
    
    -- Execution tracking
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0,
    
    -- Completion state
    pr_url TEXT,
    error TEXT,
    session_id TEXT,
    
    -- Infrastructure
    pod_name TEXT,
    
    -- Discord context
    discord_message_id TEXT,
    discord_channel_id TEXT,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Foreign key index (postgres doesn't auto-index FKs)
CREATE INDEX IF NOT EXISTS idx_minions_user_id ON minions(user_id);

COMMENT ON TABLE minions IS 'Minion task instances with full lifecycle tracking';
COMMENT ON COLUMN minions.status IS 'pending | awaiting_clarification | running | completed | failed | terminated';
COMMENT ON COLUMN minions.clarification_message_id IS 'Discord message ID for reply lookup';
COMMENT ON COLUMN minions.cost_usd IS 'Accumulated cost in USD (up to 6 decimal places)';
COMMENT ON COLUMN minions.session_id IS 'OpenCode session ID for the task';
COMMENT ON COLUMN minions.pod_name IS 'Kubernetes pod name when running';
COMMENT ON COLUMN minions.last_activity_at IS 'Updated on each event for watchdog idle detection';
