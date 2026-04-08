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

// User represents a user in the system (Discord, Matrix, or GitHub).
type User struct {
	ID              uuid.UUID
	DiscordID       *string // Discord user ID (nullable for non-Discord users)
	DiscordUsername *string // Discord username (nullable for non-Discord users)
	MatrixID        *string // Matrix user ID (e.g., @user:matrix.org)
	GitHubID        *string // GitHub user ID (numeric as text)
	GitHubUsername  *string // GitHub username
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
		if user.DiscordUsername == nil || *user.DiscordUsername != discordUsername {
			_, err = s.pool.Exec(ctx,
				`UPDATE users SET discord_username = $1, updated_at = NOW() WHERE id = $2`,
				discordUsername, user.ID)
			if err != nil {
				return nil, false, err
			}
			user.DiscordUsername = &discordUsername
		}
		return user, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	// User doesn't exist, create new one
	user = &User{
		ID:              uuid.New(),
		DiscordID:       &discordID,
		DiscordUsername: &discordUsername,
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
		`SELECT id, discord_id, discord_username, matrix_id, github_id, github_username, avatar_url, created_at, updated_at
		 FROM users WHERE discord_id = $1`,
		discordID,
	).Scan(&user.ID, &user.DiscordID, &user.DiscordUsername, &user.MatrixID, &user.GitHubID, &user.GitHubUsername, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetByMatrixID finds a user by their Matrix ID.
func (s *UserStore) GetByMatrixID(ctx context.Context, matrixID string) (*User, error) {
	user := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, discord_id, discord_username, matrix_id, github_id, github_username, avatar_url, created_at, updated_at
		 FROM users WHERE matrix_id = $1`,
		matrixID,
	).Scan(&user.ID, &user.DiscordID, &user.DiscordUsername, &user.MatrixID, &user.GitHubID, &user.GitHubUsername, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetOrCreateByMatrixID finds an existing user by Matrix ID or creates a new one.
// Returns the user and whether it was newly created.
func (s *UserStore) GetOrCreateByMatrixID(ctx context.Context, matrixID string) (*User, bool, error) {
	// Try to find existing user first
	user, err := s.GetByMatrixID(ctx, matrixID)
	if err == nil {
		return user, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	// User doesn't exist, create new one
	// Note: discord_id/discord_username are NULL for Matrix-only users
	user = &User{
		ID:       uuid.New(),
		MatrixID: &matrixID,
	}

	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (id, matrix_id)
		 VALUES ($1, $2)
		 RETURNING id, created_at, updated_at`,
		user.ID, matrixID,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}

// ErrNotFound indicates the requested resource was not found.
var ErrNotFound = errors.New("not found")

// GetByGitHubID finds a user by their GitHub ID.
func (s *UserStore) GetByGitHubID(ctx context.Context, githubID string) (*User, error) {
	user := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, discord_id, discord_username, matrix_id, github_id, github_username, avatar_url, created_at, updated_at
		 FROM users WHERE github_id = $1`,
		githubID,
	).Scan(&user.ID, &user.DiscordID, &user.DiscordUsername, &user.MatrixID, &user.GitHubID, &user.GitHubUsername, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetOrCreateByGitHubID finds an existing user by GitHub ID or creates a new one.
// Returns the user and whether it was newly created.
func (s *UserStore) GetOrCreateByGitHubID(ctx context.Context, githubID, githubUsername string) (*User, bool, error) {
	// Try to find existing user first
	user, err := s.GetByGitHubID(ctx, githubID)
	if err == nil {
		// User exists, update username if changed
		if user.GitHubUsername == nil || *user.GitHubUsername != githubUsername {
			_, err = s.pool.Exec(ctx,
				`UPDATE users SET github_username = $1, updated_at = NOW() WHERE id = $2`,
				githubUsername, user.ID)
			if err != nil {
				return nil, false, err
			}
			user.GitHubUsername = &githubUsername
		}
		return user, false, nil
	}

	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	// User doesn't exist, create new one
	// Note: No ON CONFLICT needed since we already checked for existing user above.
	// The unique index on github_id will prevent duplicates in race conditions.
	// discord_id/discord_username are NULL for GitHub-only users
	user = &User{
		ID:             uuid.New(),
		GitHubID:       &githubID,
		GitHubUsername: &githubUsername,
	}

	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (id, github_id, github_username)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		user.ID, githubID, githubUsername,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}
