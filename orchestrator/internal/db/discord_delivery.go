package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// findDiscordDelivery looks up a replay after serializing submissions for the same message.
func findDiscordDelivery(ctx context.Context, tx pgx.Tx, params CreateMinionParams) (*FindDuplicateResult, error) {
	lockKey := repoTaskHash("discord-delivery", params.DiscordMessageID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
		return nil, fmt.Errorf("lock Discord delivery: %w", err)
	}

	minion, err := scanMinion(tx.QueryRow(ctx, `SELECT `+minionSelectColumns+`
		FROM minions
		WHERE platform = 'discord' AND discord_message_id = $1 AND user_id = $2
		ORDER BY created_at, id LIMIT 1`, params.DiscordMessageID, params.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return &FindDuplicateResult{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Discord delivery: %w", err)
	}

	return &FindDuplicateResult{Found: true, ExistingMinion: minion}, nil
}
