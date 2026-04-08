-- Migration: 010_add_github_fields.sql
-- Adds GitHub user identity and PR feedback flow support

-- Users: add GitHub identity (parallel to Discord/Matrix pattern)
ALTER TABLE users ADD COLUMN IF NOT EXISTS github_id TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS github_username TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id) WHERE github_id IS NOT NULL;

-- Minions: add branch for follow-up minions (clone specific branch instead of default)
ALTER TABLE minions ADD COLUMN IF NOT EXISTS branch TEXT;

-- Minions: add source PR URL (set at creation time for PR feedback flow)
-- This is different from pr_url which is set at completion time
ALTER TABLE minions ADD COLUMN IF NOT EXISTS source_pr_url TEXT;

-- Minions: add GitHub comment ID for emoji reaction tracking
ALTER TABLE minions ADD COLUMN IF NOT EXISTS github_comment_id TEXT;

-- Platform: add 'github' option for PR feedback minions
ALTER TABLE minions DROP CONSTRAINT IF EXISTS minions_platform_check;
ALTER TABLE minions ADD CONSTRAINT minions_platform_check CHECK (
    platform IN ('discord', 'matrix', 'github')
);

-- Index for "is there an active minion for this PR?" queries
-- Used to enforce one minion per PR at a time
CREATE INDEX IF NOT EXISTS idx_minions_source_pr_active 
    ON minions(source_pr_url) 
    WHERE source_pr_url IS NOT NULL AND status IN ('pending', 'running');

COMMENT ON COLUMN users.github_id IS 'GitHub user ID (numeric as text)';
COMMENT ON COLUMN users.github_username IS 'GitHub username at time of last interaction';
COMMENT ON COLUMN minions.branch IS 'Target branch to clone (for PR feedback flow)';
COMMENT ON COLUMN minions.source_pr_url IS 'PR URL this minion is addressing (set at creation, not completion)';
COMMENT ON COLUMN minions.github_comment_id IS 'GitHub comment ID that triggered this minion';
