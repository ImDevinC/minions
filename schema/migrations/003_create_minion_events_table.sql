-- Migration: 003_create_minion_events_table.sql
-- Creates append-only event log for minion activity

CREATE TABLE IF NOT EXISTS minion_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    minion_id UUID NOT NULL REFERENCES minions(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_type TEXT NOT NULL,
    content JSONB NOT NULL DEFAULT '{}'
);

-- Composite index for efficient event retrieval by minion + time ordering
-- This is the primary query pattern: fetch events for a minion in chronological order
CREATE INDEX IF NOT EXISTS idx_minion_events_minion_id_timestamp 
    ON minion_events(minion_id, timestamp);

COMMENT ON TABLE minion_events IS 'Append-only event log for minion activity streaming';
COMMENT ON COLUMN minion_events.event_type IS 'Event type (e.g., message, tool_call, token_usage, error)';
COMMENT ON COLUMN minion_events.content IS 'Event payload as JSONB for flexible schema';
