-- Migration: 007_add_token_detail_fields.sql
-- Adds granular token tracking fields for detailed cost attribution
-- New fields: reasoning_tokens, cache_read_tokens, cache_write_tokens
-- All default to 0 to ensure existing minions get zero-initialized values

ALTER TABLE minions ADD COLUMN reasoning_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE minions ADD COLUMN cache_read_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE minions ADD COLUMN cache_write_tokens BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN minions.reasoning_tokens IS 'Extended thinking tokens (Claude 3.7+ sonnet models)';
COMMENT ON COLUMN minions.cache_read_tokens IS 'Prompt cache read tokens (cheaper than input)';
COMMENT ON COLUMN minions.cache_write_tokens IS 'Prompt cache write tokens (same cost as output)';
