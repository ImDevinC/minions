CREATE INDEX IF NOT EXISTS idx_minions_discord_delivery
    ON minions (discord_message_id, user_id, created_at, id)
    WHERE platform = 'discord' AND discord_message_id IS NOT NULL;
