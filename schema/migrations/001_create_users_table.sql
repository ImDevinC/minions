-- Migration: 001_create_users_table.sql
-- Creates the users table for Discord OAuth

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    discord_id TEXT NOT NULL,
    discord_username TEXT NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_discord_id_unique UNIQUE (discord_id)
);

-- Index for lookups by discord_id (covered by unique constraint, but explicit for clarity)
CREATE INDEX IF NOT EXISTS idx_users_discord_id ON users(discord_id);

COMMENT ON TABLE users IS 'Users authenticated via Discord OAuth';
COMMENT ON COLUMN users.discord_id IS 'Discord user ID (snowflake as text)';
COMMENT ON COLUMN users.discord_username IS 'Discord username at time of last login';
COMMENT ON COLUMN users.avatar_url IS 'Discord avatar URL, nullable';
