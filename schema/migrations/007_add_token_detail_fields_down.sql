-- Migration: 007_add_token_detail_fields_down.sql
-- Rollback migration for 007_add_token_detail_fields.sql
-- Removes granular token tracking fields

ALTER TABLE minions DROP COLUMN reasoning_tokens;
ALTER TABLE minions DROP COLUMN cache_read_tokens;
ALTER TABLE minions DROP COLUMN cache_write_tokens;
