-- Migration: 004_create_indexes.sql
-- Creates indexes for common query patterns

-- Status filtering: GET /api/minions?status=running
CREATE INDEX IF NOT EXISTS idx_minions_status ON minions(status);

-- Watchdog query: find running minions with stale activity
-- Composite index covers: WHERE status = 'running' AND last_activity_at < ?
CREATE INDEX IF NOT EXISTS idx_minions_status_last_activity_at ON minions(status, last_activity_at);

-- Clarification reply lookup: find minion by Discord message ID
-- Used when user replies to a clarification question
CREATE INDEX IF NOT EXISTS idx_minions_clarification_message_id ON minions(clarification_message_id)
    WHERE clarification_message_id IS NOT NULL;

COMMENT ON INDEX idx_minions_status IS 'Speeds up status filtering on list endpoints';
COMMENT ON INDEX idx_minions_status_last_activity_at IS 'Watchdog query: running minions with stale last_activity_at';
COMMENT ON INDEX idx_minions_clarification_message_id IS 'Partial index for clarification reply lookup';
