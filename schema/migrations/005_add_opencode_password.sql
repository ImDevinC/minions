-- Add opencode_password column for per-minion SSE authentication
-- Nullable: passwords only exist during active execution
ALTER TABLE minions ADD COLUMN opencode_password TEXT NULL;
