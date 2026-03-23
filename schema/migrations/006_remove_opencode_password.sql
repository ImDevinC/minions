-- Remove opencode_password column (no longer using password authentication for SSE)
-- SSE now relies on trusted pod network instead of per-minion credentials
ALTER TABLE minions DROP COLUMN IF EXISTS opencode_password;
