// Package db provides database connectivity and repositories.
package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents a Discord user in the system.
type User struct {
	ID              uuid.UUID
	DiscordID       string
	DiscordUsername string
	AvatarURL       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UserStore handles user database operations.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore creates a new UserStore.
func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

// GetOrCreate finds an existing user by Discord ID or creates a new one.
// Returns the user and whether it was newly created.
func (s *UserStore) GetOrCreate(ctx context.Context, discordID, discordUsername string) (*User, bool, error) {
	// Try to find existing user first
	user, err := s.GetByDiscordID(ctx, discordID)
	if err == nil {
		// User exists, update username if changed
		if user.DiscordUsername != discordUsername {
			_, err = s.pool.Exec(ctx,
				`UPDATE users SET discord_username = $1, updated_at = NOW() WHERE id = $2`,
				discordUsername, user.ID)
			if err != nil {
				return nil, false, err
			}
			user.DiscordUsername = discordUsername
		}
		return user, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	// User doesn't exist, create new one
	user = &User{
		ID:              uuid.New(),
		DiscordID:       discordID,
		DiscordUsername: discordUsername,
	}

	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (id, discord_id, discord_username)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (discord_id) DO UPDATE SET
		   discord_username = EXCLUDED.discord_username,
		   updated_at = NOW()
		 RETURNING id, created_at, updated_at`,
		user.ID, discordID, discordUsername,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}

// GetByDiscordID finds a user by their Discord ID.
func (s *UserStore) GetByDiscordID(ctx context.Context, discordID string) (*User, error) {
	user := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, discord_id, discord_username, avatar_url, created_at, updated_at
		 FROM users WHERE discord_id = $1`,
		discordID,
	).Scan(&user.ID, &user.DiscordID, &user.DiscordUsername, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// ErrNotFound indicates the requested resource was not found.
var ErrNotFound = errors.New("not found")
