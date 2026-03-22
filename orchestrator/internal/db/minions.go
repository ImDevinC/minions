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

// MinionStatus represents the lifecycle state of a minion.
type MinionStatus string

const (
	StatusPending               MinionStatus = "pending"
	StatusAwaitingClarification MinionStatus = "awaiting_clarification"
	StatusRunning               MinionStatus = "running"
	StatusCompleted             MinionStatus = "completed"
	StatusFailed                MinionStatus = "failed"
	StatusTerminated            MinionStatus = "terminated"
)

// Minion represents a task execution instance.
type Minion struct {
	ID                     uuid.UUID
	UserID                 uuid.UUID
	Repo                   string
	Task                   string
	Model                  string
	Status                 MinionStatus
	ClarificationQuestion  *string
	ClarificationAnswer    *string
	ClarificationMessageID *string
	InputTokens            int64
	OutputTokens           int64
	CostUSD                float64
	PRURL                  *string
	Error                  *string
	SessionID              *string
	PodName                *string
	DiscordMessageID       *string
	DiscordChannelID       *string
	CreatedAt              time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	LastActivityAt         time.Time
}

// CreateMinionParams holds parameters for creating a new minion.
type CreateMinionParams struct {
	UserID           uuid.UUID
	Repo             string
	Task             string
	Model            string
	DiscordMessageID string
	DiscordChannelID string
}

// MinionStore handles minion database operations.
type MinionStore struct {
	pool *pgxpool.Pool
}

// NewMinionStore creates a new MinionStore.
func NewMinionStore(pool *pgxpool.Pool) *MinionStore {
	return &MinionStore{pool: pool}
}

// Create inserts a new minion record.
func (s *MinionStore) Create(ctx context.Context, params CreateMinionParams) (*Minion, error) {
	minion := &Minion{
		ID:     uuid.New(),
		UserID: params.UserID,
		Repo:   params.Repo,
		Task:   params.Task,
		Model:  params.Model,
		Status: StatusPending,
	}

	if params.DiscordMessageID != "" {
		minion.DiscordMessageID = &params.DiscordMessageID
	}
	if params.DiscordChannelID != "" {
		minion.DiscordChannelID = &params.DiscordChannelID
	}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO minions (id, user_id, repo, task, model, status, discord_message_id, discord_channel_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING created_at, last_activity_at`,
		minion.ID, minion.UserID, minion.Repo, minion.Task, minion.Model, minion.Status,
		minion.DiscordMessageID, minion.DiscordChannelID,
	).Scan(&minion.CreatedAt, &minion.LastActivityAt)

	if err != nil {
		return nil, err
	}

	return minion, nil
}

// GetByID retrieves a minion by ID.
func (s *MinionStore) GetByID(ctx context.Context, id uuid.UUID) (*Minion, error) {
	minion := &Minion{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions WHERE id = $1`,
		id,
	).Scan(
		&minion.ID, &minion.UserID, &minion.Repo, &minion.Task, &minion.Model, &minion.Status,
		&minion.ClarificationQuestion, &minion.ClarificationAnswer, &minion.ClarificationMessageID,
		&minion.InputTokens, &minion.OutputTokens, &minion.CostUSD,
		&minion.PRURL, &minion.Error, &minion.SessionID, &minion.PodName,
		&minion.DiscordMessageID, &minion.DiscordChannelID,
		&minion.CreatedAt, &minion.StartedAt, &minion.CompletedAt, &minion.LastActivityAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return minion, nil
}

// RateLimitInfo contains rate limit counters for a user.
type RateLimitInfo struct {
	HourlyCount     int
	ConcurrentCount int
}

// GetRateLimitInfo returns rate limit counters for a user.
// HourlyCount: minions created in the last hour
// ConcurrentCount: minions in pending or running state
func (s *MinionStore) GetRateLimitInfo(ctx context.Context, userID uuid.UUID) (*RateLimitInfo, error) {
	info := &RateLimitInfo{}

	// Count minions created in the last hour
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM minions
		 WHERE user_id = $1 AND created_at > NOW() - INTERVAL '1 hour'`,
		userID,
	).Scan(&info.HourlyCount)
	if err != nil {
		return nil, err
	}

	// Count pending or running minions
	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM minions
		 WHERE user_id = $1 AND status IN ('pending', 'running')`,
		userID,
	).Scan(&info.ConcurrentCount)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// RateLimits defines the rate limiting thresholds.
const (
	MaxMinionsPerHour    = 10
	MaxConcurrentMinions = 3
)

// ErrRateLimitExceeded indicates the user has exceeded their rate limit.
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// ErrConcurrentLimitExceeded indicates the user has too many concurrent minions.
var ErrConcurrentLimitExceeded = errors.New("concurrent limit exceeded")

// ListMinionsParams holds parameters for listing minions.
type ListMinionsParams struct {
	Status *MinionStatus // optional filter by status
	Limit  int           // max results, 0 means default (50)
}

const defaultListLimit = 50
const maxListLimit = 200

// List returns minions ordered by created_at desc with optional filters.
func (s *MinionStore) List(ctx context.Context, params ListMinionsParams) ([]*Minion, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	// Build query dynamically based on filters
	query := `SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions`

	var args []any
	argIdx := 1

	if params.Status != nil {
		query += " WHERE status = $1"
		args = append(args, *params.Status)
		argIdx++
	}

	query += " ORDER BY created_at DESC LIMIT $" + itoa(argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var minions []*Minion
	for rows.Next() {
		m := &Minion{}
		err := rows.Scan(
			&m.ID, &m.UserID, &m.Repo, &m.Task, &m.Model, &m.Status,
			&m.ClarificationQuestion, &m.ClarificationAnswer, &m.ClarificationMessageID,
			&m.InputTokens, &m.OutputTokens, &m.CostUSD,
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName,
			&m.DiscordMessageID, &m.DiscordChannelID,
			&m.CreatedAt, &m.StartedAt, &m.CompletedAt, &m.LastActivityAt,
		)
		if err != nil {
			return nil, err
		}
		minions = append(minions, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return minions, nil
}

// itoa converts int to string without importing strconv (tiny helper).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
