-- Migration: 011_fix_user_constraints.sql
-- Fix user table constraints to support multi-platform users (Discord, Matrix, GitHub)
-- Previously discord_id was NOT NULL which breaks when creating Matrix/GitHub-only users

-- Make discord_id nullable for non-Discord users
ALTER TABLE users ALTER COLUMN discord_id DROP NOT NULL;
ALTER TABLE users ALTER COLUMN discord_username DROP NOT NULL;

-- Update existing empty strings to NULL
UPDATE users SET discord_id = NULL WHERE discord_id = '';
UPDATE users SET discord_username = NULL WHERE discord_username = '';

-- Drop the old unique constraint that doesn't allow NULL
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_discord_id_unique;

-- Create a partial unique index that only enforces uniqueness for non-NULL values
-- This allows multiple users to have NULL discord_id (Matrix/GitHub users)
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_discord_id_unique 
    ON users(discord_id) WHERE discord_id IS NOT NULL;

-- Also fix matrix_id constraint - drop the full constraint and use partial index
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_matrix_id_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_matrix_id_unique 
    ON users(matrix_id) WHERE matrix_id IS NOT NULL;

COMMENT ON COLUMN users.discord_id IS 'Discord user ID (snowflake as text), NULL for non-Discord users';
COMMENT ON COLUMN users.discord_username IS 'Discord username at time of last login, NULL for non-Discord users';
