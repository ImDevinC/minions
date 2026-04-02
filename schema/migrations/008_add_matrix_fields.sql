-- Migration: 008_add_matrix_fields.sql
-- Adds Matrix-specific fields alongside Discord fields for multi-platform bot support

-- Matrix context fields (parallel to Discord fields)
ALTER TABLE minions ADD COLUMN IF NOT EXISTS matrix_event_id TEXT;
ALTER TABLE minions ADD COLUMN IF NOT EXISTS matrix_room_id TEXT;

-- Platform indicator to route webhook notifications
-- 'discord' = existing Discord bot, 'matrix' = new Matrix bot
ALTER TABLE minions ADD COLUMN IF NOT EXISTS platform TEXT NOT NULL DEFAULT 'discord' CHECK (
    platform IN ('discord', 'matrix')
);

-- Matrix clarification message tracking (parallel to clarification_message_id)
ALTER TABLE minions ADD COLUMN IF NOT EXISTS matrix_clarification_event_id TEXT;

-- Index for Matrix clarification lookup (parallel to Discord)
CREATE INDEX IF NOT EXISTS idx_minions_matrix_clarification_event_id 
    ON minions(matrix_clarification_event_id) 
    WHERE matrix_clarification_event_id IS NOT NULL;

-- Index for platform-based queries
CREATE INDEX IF NOT EXISTS idx_minions_platform ON minions(platform);

-- Update users table to support Matrix user IDs
ALTER TABLE users ADD COLUMN IF NOT EXISTS matrix_id TEXT;

-- Index for Matrix user lookup
CREATE INDEX IF NOT EXISTS idx_users_matrix_id ON users(matrix_id) WHERE matrix_id IS NOT NULL;

COMMENT ON COLUMN minions.matrix_event_id IS 'Matrix event ID of the original command message';
COMMENT ON COLUMN minions.matrix_room_id IS 'Matrix room ID for sending notifications';
COMMENT ON COLUMN minions.platform IS 'Platform where minion was created: discord or matrix';
COMMENT ON COLUMN minions.matrix_clarification_event_id IS 'Matrix event ID for clarification reply lookup';
COMMENT ON COLUMN users.matrix_id IS 'Matrix user ID (e.g., @user:matrix.org)';
