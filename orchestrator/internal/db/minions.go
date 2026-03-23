// Package db provides database connectivity and repositories.
package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool defines the interface for database connection pooling.
// Both *pgxpool.Pool and mock implementations satisfy this interface.
type Pool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

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
	OpencodePassword       string
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
	pool Pool
}

// NewMinionStore creates a new MinionStore.
func NewMinionStore(pool *pgxpool.Pool) *MinionStore {
	return &MinionStore{pool: pool}
}

// NewMinionStoreWithPool creates a MinionStore with a custom Pool implementation.
// This is useful for testing with mock pools.
func NewMinionStoreWithPool(pool Pool) *MinionStore {
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

// repoTaskHash generates a 64-bit hash for use with pg_advisory_lock.
// Uses SHA-256 truncated to 64 bits for collision resistance.
func repoTaskHash(repo, task string) int64 {
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0}) // separator
	h.Write([]byte(task))
	sum := h.Sum(nil)
	// Take first 8 bytes as int64
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// FindDuplicateResult holds the result of a duplicate check.
type FindDuplicateResult struct {
	// Found indicates whether a duplicate was found.
	Found bool
	// ExistingMinion is the existing minion if found.
	ExistingMinion *Minion
}

// FindRecentDuplicate checks for a duplicate minion with the same repo+task in the last DuplicateWindow.
// Uses pg_advisory_xact_lock to prevent race conditions during creation.
// Must be called within a transaction for the lock to be effective.
// Returns nil if no duplicate found.
func (s *MinionStore) FindRecentDuplicate(ctx context.Context, tx pgx.Tx, repo, task string) (*FindDuplicateResult, error) {
	// Acquire advisory lock based on repo+task hash to serialize duplicate checks
	lockKey := repoTaskHash(repo, task)
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey)
	if err != nil {
		return nil, err
	}

	// Check for existing minion with same repo+task in the duplicate window
	var minion Minion
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions 
		 WHERE repo = $1 AND task = $2 AND created_at > NOW() - $3::interval
		 ORDER BY created_at DESC
		 LIMIT 1`,
		repo, task, DuplicateWindow.String(),
	).Scan(
		&minion.ID, &minion.UserID, &minion.Repo, &minion.Task, &minion.Model, &minion.Status,
		&minion.ClarificationQuestion, &minion.ClarificationAnswer, &minion.ClarificationMessageID,
		&minion.InputTokens, &minion.OutputTokens, &minion.CostUSD,
		&minion.PRURL, &minion.Error, &minion.SessionID, &minion.PodName, &minion.OpencodePassword,
		&minion.DiscordMessageID, &minion.DiscordChannelID,
		&minion.CreatedAt, &minion.StartedAt, &minion.CompletedAt, &minion.LastActivityAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return &FindDuplicateResult{Found: false}, nil
	}
	if err != nil {
		return nil, err
	}

	return &FindDuplicateResult{Found: true, ExistingMinion: &minion}, nil
}

// CreateOrFindDuplicateResult holds the result of CreateOrFindDuplicate.
type CreateOrFindDuplicateResult struct {
	// Minion is the created or existing minion.
	Minion *Minion
	// WasDuplicate indicates whether an existing minion was returned.
	WasDuplicate bool
}

// CreateOrFindDuplicate creates a new minion or returns an existing duplicate.
// Uses pg_advisory_xact_lock to prevent race conditions.
// If a minion with the same repo+task exists within DuplicateWindow (5 min), returns it instead.
//
// TODO: Add --force flag support to bypass duplicate detection when explicitly requested.
func (s *MinionStore) CreateOrFindDuplicate(ctx context.Context, params CreateMinionParams) (*CreateOrFindDuplicateResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Check for duplicate (with advisory lock held)
	dupResult, err := s.FindRecentDuplicate(ctx, tx, params.Repo, params.Task)
	if err != nil {
		return nil, err
	}

	if dupResult.Found {
		// Return existing minion (no need to commit, nothing changed)
		return &CreateOrFindDuplicateResult{
			Minion:       dupResult.ExistingMinion,
			WasDuplicate: true,
		}, nil
	}

	// Create new minion within the same transaction (lock still held)
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

	err = tx.QueryRow(ctx,
		`INSERT INTO minions (id, user_id, repo, task, model, status, discord_message_id, discord_channel_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING created_at, last_activity_at`,
		minion.ID, minion.UserID, minion.Repo, minion.Task, minion.Model, minion.Status,
		minion.DiscordMessageID, minion.DiscordChannelID,
	).Scan(&minion.CreatedAt, &minion.LastActivityAt)

	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &CreateOrFindDuplicateResult{
		Minion:       minion,
		WasDuplicate: false,
	}, nil
}

// GetByID retrieves a minion by ID.
func (s *MinionStore) GetByID(ctx context.Context, id uuid.UUID) (*Minion, error) {
	minion := &Minion{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions WHERE id = $1`,
		id,
	).Scan(
		&minion.ID, &minion.UserID, &minion.Repo, &minion.Task, &minion.Model, &minion.Status,
		&minion.ClarificationQuestion, &minion.ClarificationAnswer, &minion.ClarificationMessageID,
		&minion.InputTokens, &minion.OutputTokens, &minion.CostUSD,
		&minion.PRURL, &minion.Error, &minion.SessionID, &minion.PodName, &minion.OpencodePassword,
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

// DuplicateWindow is the time window for duplicate detection.
const DuplicateWindow = 5 * time.Minute

// ErrDuplicateMinion indicates a duplicate minion was found.
var ErrDuplicateMinion = errors.New("duplicate minion found")

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
		        pr_url, error, session_id, pod_name, opencode_password,
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
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
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

// TerminateResult holds the result of a terminate operation.
type TerminateResult struct {
	// WasTerminated indicates whether the minion was actually terminated
	// (vs already being in a terminal state).
	WasTerminated bool
	// PreviousStatus is the status before termination (for idempotency checks).
	PreviousStatus MinionStatus
	// PodName is the pod name if one was assigned (for k8s cleanup).
	PodName *string
	// DiscordChannelID for sending notifications.
	DiscordChannelID *string
}

// ErrAlreadyTerminal indicates the minion is already in a terminal state.
var ErrAlreadyTerminal = errors.New("minion already in terminal state")

// Terminate atomically updates a minion's status to 'terminated'.
// Uses a transaction to check status before update, handling concurrent requests.
// Returns ErrNotFound if minion doesn't exist.
// Returns TerminateResult with WasTerminated=false if already terminal (idempotent).
func (s *MinionStore) Terminate(ctx context.Context, id uuid.UUID) (*TerminateResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and fetch current status
	var currentStatus MinionStatus
	var podName, discordChannelID *string
	err = tx.QueryRow(ctx,
		`SELECT status, pod_name, discord_channel_id FROM minions WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&currentStatus, &podName, &discordChannelID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	result := &TerminateResult{
		PreviousStatus:   currentStatus,
		PodName:          podName,
		DiscordChannelID: discordChannelID,
	}

	// If already in a terminal state, return success (idempotent)
	if currentStatus == StatusCompleted || currentStatus == StatusFailed || currentStatus == StatusTerminated {
		result.WasTerminated = false
		return result, nil
	}

	// Update to terminated
	_, err = tx.Exec(ctx,
		`UPDATE minions SET status = $1, completed_at = NOW(), last_activity_at = NOW() WHERE id = $2`,
		StatusTerminated, id,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	result.WasTerminated = true
	return result, nil
}

// CompleteParams holds parameters for completing a minion.
type CompleteParams struct {
	ID        uuid.UUID
	Status    MinionStatus // must be completed or failed
	PRURL     *string      // optional, for completed minions
	Error     *string      // optional, for failed minions
	SessionID *string      // optional, opencode session ID
}

// CompleteResult holds the result of a complete operation.
type CompleteResult struct {
	// WasUpdated indicates whether the minion was actually updated
	// (vs already being in a terminal state).
	WasUpdated bool
	// PreviousStatus is the status before update.
	PreviousStatus MinionStatus
	// DiscordChannelID for sending notifications.
	DiscordChannelID *string
}

// Complete marks a minion as completed or failed.
// Uses a transaction to atomically check and update status.
// Returns ErrNotFound if minion doesn't exist.
// Returns CompleteResult with WasUpdated=false if already terminal (idempotent).
func (s *MinionStore) Complete(ctx context.Context, params CompleteParams) (*CompleteResult, error) {
	// Validate status
	if params.Status != StatusCompleted && params.Status != StatusFailed {
		return nil, errors.New("status must be completed or failed")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and fetch current status
	var currentStatus MinionStatus
	var discordChannelID *string
	err = tx.QueryRow(ctx,
		`SELECT status, discord_channel_id FROM minions WHERE id = $1 FOR UPDATE`,
		params.ID,
	).Scan(&currentStatus, &discordChannelID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	result := &CompleteResult{
		PreviousStatus:   currentStatus,
		DiscordChannelID: discordChannelID,
	}

	// If already in a terminal state, return success (idempotent)
	if currentStatus == StatusCompleted || currentStatus == StatusFailed || currentStatus == StatusTerminated {
		result.WasUpdated = false
		return result, nil
	}

	// Update to completed/failed
	_, err = tx.Exec(ctx,
		`UPDATE minions SET 
			status = $1, 
			pr_url = $2, 
			error = $3, 
			session_id = $4,
			completed_at = NOW(), 
			last_activity_at = NOW() 
		WHERE id = $5`,
		params.Status, params.PRURL, params.Error, params.SessionID, params.ID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	result.WasUpdated = true
	return result, nil
}

// Stats contains aggregate statistics for all minions.
type Stats struct {
	TotalCostUSD      float64      `json:"total_cost_usd"`
	TotalInputTokens  int64        `json:"total_input_tokens"`
	TotalOutputTokens int64        `json:"total_output_tokens"`
	ByModel           []ModelStats `json:"by_model"`
}

// ModelStats contains statistics for a specific model.
type ModelStats struct {
	Model        string  `json:"model"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Count        int64   `json:"count"`
}

// SetClarificationParams holds parameters for setting clarification state.
type SetClarificationParams struct {
	ID                     uuid.UUID
	Question               string
	ClarificationMessageID string
}

// SetClarification updates a minion's clarification state and sets status to awaiting_clarification.
// Uses a transaction to atomically check and update status.
// Returns ErrNotFound if minion doesn't exist.
// Only allows transition from pending status.
func (s *MinionStore) SetClarification(ctx context.Context, params SetClarificationParams) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and fetch current status
	var currentStatus MinionStatus
	err = tx.QueryRow(ctx,
		`SELECT status FROM minions WHERE id = $1 FOR UPDATE`,
		params.ID,
	).Scan(&currentStatus)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Only allow transition from pending
	if currentStatus != StatusPending {
		return ErrInvalidStatusTransition
	}

	// Update to awaiting_clarification with question
	_, err = tx.Exec(ctx,
		`UPDATE minions SET 
			status = $1,
			clarification_question = $2,
			clarification_message_id = $3,
			last_activity_at = NOW()
		WHERE id = $4`,
		StatusAwaitingClarification, params.Question, params.ClarificationMessageID, params.ID,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ErrInvalidStatusTransition indicates an invalid status transition was attempted.
var ErrInvalidStatusTransition = errors.New("invalid status transition")

// ListByStatuses returns minions with any of the given statuses.
// Used for reconciliation queries (e.g., find all pending/running minions).
func (s *MinionStore) ListByStatuses(ctx context.Context, statuses []MinionStatus) ([]*Minion, error) {
	if len(statuses) == 0 {
		return []*Minion{}, nil
	}

	// Build query with IN clause
	query := `SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions WHERE status = ANY($1)`

	// Convert to []string for pgx array handling
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = string(s)
	}

	rows, err := s.pool.Query(ctx, query, statusStrings)
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
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
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

// ListPending returns minions in pending status ordered by created_at ASC (FIFO).
// Used by spawner to process minions in order of creation.
func (s *MinionStore) ListPending(ctx context.Context) ([]*Minion, error) {
	query := `SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions 
		 WHERE status = $1
		 ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, query, StatusPending)
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
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
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

// MarkFailed marks a minion as failed with the given error message.
// Used by reconciliation to mark orphaned minions.
func (s *MinionStore) MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET 
			status = $1, 
			error = $2, 
			completed_at = NOW(), 
			last_activity_at = NOW() 
		WHERE id = $3 AND status NOT IN ($4, $5, $6)`,
		StatusFailed, errorMsg, id, StatusCompleted, StatusFailed, StatusTerminated,
	)
	return err
}

// UpdateTokenUsageParams holds parameters for updating token usage.
type UpdateTokenUsageParams struct {
	ID           uuid.UUID
	InputTokens  int64
	OutputTokens int64
}

// UpdateTokenUsage atomically adds to a minion's token usage counters.
// Also updates last_activity_at to track pod activity.
func (s *MinionStore) UpdateTokenUsage(ctx context.Context, params UpdateTokenUsageParams) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET 
			input_tokens = input_tokens + $1,
			output_tokens = output_tokens + $2,
			last_activity_at = NOW()
		WHERE id = $3`,
		params.InputTokens, params.OutputTokens, params.ID,
	)
	return err
}

// ListIdleRunning returns running minions with last_activity_at older than threshold.
// Used by watchdog to detect idle minions that may be stuck.
func (s *MinionStore) ListIdleRunning(ctx context.Context, idleThreshold time.Duration) ([]*Minion, error) {
	query := `SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions 
		 WHERE status = 'running' 
		   AND last_activity_at < NOW() - $1::interval`

	rows, err := s.pool.Query(ctx, query, idleThreshold.String())
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
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
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

// ListClarificationTimeouts returns minions stuck in awaiting_clarification for too long.
// Used by watchdog to enforce clarification timeout (24h by default).
func (s *MinionStore) ListClarificationTimeouts(ctx context.Context, timeout time.Duration) ([]*Minion, error) {
	query := `SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions 
		 WHERE status = 'awaiting_clarification' 
		   AND created_at < NOW() - $1::interval`

	rows, err := s.pool.Query(ctx, query, timeout.String())
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
			&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
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

// GetByClarificationMessageID looks up a minion by its Discord clarification message ID.
// Used for processing replies to clarification questions.
func (s *MinionStore) GetByClarificationMessageID(ctx context.Context, messageID string) (*Minion, error) {
	m := &Minion{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, repo, task, model, status,
		        clarification_question, clarification_answer, clarification_message_id,
		        input_tokens, output_tokens, cost_usd,
		        pr_url, error, session_id, pod_name, opencode_password,
		        discord_message_id, discord_channel_id,
		        created_at, started_at, completed_at, last_activity_at
		 FROM minions WHERE clarification_message_id = $1`,
		messageID,
	).Scan(
		&m.ID, &m.UserID, &m.Repo, &m.Task, &m.Model, &m.Status,
		&m.ClarificationQuestion, &m.ClarificationAnswer, &m.ClarificationMessageID,
		&m.InputTokens, &m.OutputTokens, &m.CostUSD,
		&m.PRURL, &m.Error, &m.SessionID, &m.PodName, &m.OpencodePassword,
		&m.DiscordMessageID, &m.DiscordChannelID,
		&m.CreatedAt, &m.StartedAt, &m.CompletedAt, &m.LastActivityAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return m, nil
}

// SetClarificationAnswerParams holds parameters for SetClarificationAnswer.
type SetClarificationAnswerParams struct {
	ID     uuid.UUID
	Answer string
}

// SetClarificationAnswer sets the user's answer and transitions to pending (ready to spawn).
// Only valid from awaiting_clarification status.
func (s *MinionStore) SetClarificationAnswer(ctx context.Context, params SetClarificationAnswerParams) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and fetch current status
	var currentStatus MinionStatus
	err = tx.QueryRow(ctx,
		`SELECT status FROM minions WHERE id = $1 FOR UPDATE`,
		params.ID,
	).Scan(&currentStatus)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Only allow transition from awaiting_clarification
	if currentStatus != StatusAwaitingClarification {
		return ErrInvalidStatusTransition
	}

	// Update to pending with answer
	_, err = tx.Exec(ctx,
		`UPDATE minions SET 
			status = $1,
			clarification_answer = $2,
			last_activity_at = NOW()
		WHERE id = $3`,
		StatusPending, params.Answer, params.ID,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// MarkRunning transitions a minion from pending to running.
// Sets status='running', pod_name, started_at=NOW(), last_activity_at=NOW().
// Uses a transaction with FOR UPDATE row lock to prevent concurrent updates.
// Returns ErrNotFound if minion doesn't exist.
// Returns ErrInvalidStatusTransition if not in pending status.
func (s *MinionStore) MarkRunning(ctx context.Context, id uuid.UUID, podName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and fetch current status
	var currentStatus MinionStatus
	err = tx.QueryRow(ctx,
		`SELECT status FROM minions WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&currentStatus)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Only allow transition from pending
	if currentStatus != StatusPending {
		return ErrInvalidStatusTransition
	}

	// Update to running with pod_name and timestamps
	_, err = tx.Exec(ctx,
		`UPDATE minions SET 
			status = $1,
			pod_name = $2,
			started_at = NOW(),
			last_activity_at = NOW()
		WHERE id = $3`,
		StatusRunning, podName, id,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetStats returns aggregate statistics across all minions.
func (s *MinionStore) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{
		ByModel: []ModelStats{},
	}

	// Get totals
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		 FROM minions`,
	).Scan(&stats.TotalCostUSD, &stats.TotalInputTokens, &stats.TotalOutputTokens)
	if err != nil {
		return nil, err
	}

	// Get breakdown by model
	rows, err := s.pool.Query(ctx,
		`SELECT model, COALESCE(SUM(cost_usd), 0), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COUNT(*)
		 FROM minions
		 GROUP BY model
		 ORDER BY SUM(cost_usd) DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ms ModelStats
		if err := rows.Scan(&ms.Model, &ms.CostUSD, &ms.InputTokens, &ms.OutputTokens, &ms.Count); err != nil {
			return nil, err
		}
		stats.ByModel = append(stats.ByModel, ms)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

// StorePassword stores a per-minion OpenCode password for SSE authentication.
// Idempotent - safe to call multiple times with same password.
func (s *MinionStore) StorePassword(ctx context.Context, id uuid.UUID, password string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET opencode_password = $1 WHERE id = $2`,
		password, id,
	)
	return err
}

// ClearPassword removes the opencode_password when minion reaches terminal state.
// Idempotent - safe to call multiple times (NULL remains NULL).
func (s *MinionStore) ClearPassword(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET opencode_password = NULL WHERE id = $1`,
		id,
	)
	return err
}
