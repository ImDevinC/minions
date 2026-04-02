// Package db provides database connectivity and repositories.
package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
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

// Scanner is satisfied by pgx.Row and pgx.Rows (which embeds Row).
// Allows scanMinion to work with both single-row and multi-row queries.
type Scanner interface {
	Scan(dest ...any) error
}

// minionSelectColumns is the canonical SELECT column list for minion queries.
// Keep in sync with scanMinion().
const minionSelectColumns = `id, user_id, repo, task, model, status, platform,
	clarification_question, clarification_answer, clarification_message_id, matrix_clarification_event_id,
	input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens, cost_usd,
	pr_url, error, session_id, pod_name,
	discord_message_id, discord_channel_id, matrix_event_id, matrix_room_id,
	created_at, started_at, completed_at, last_activity_at`

// minionSelectColumnsWithPrefix is the prefixed version for JOINs.
const minionSelectColumnsWithPrefix = `m.id, m.user_id, m.repo, m.task, m.model, m.status, m.platform,
	m.clarification_question, m.clarification_answer, m.clarification_message_id, m.matrix_clarification_event_id,
	m.input_tokens, m.output_tokens, m.reasoning_tokens, m.cache_read_tokens, m.cache_write_tokens, m.cost_usd,
	m.pr_url, m.error, m.session_id, m.pod_name,
	m.discord_message_id, m.discord_channel_id, m.matrix_event_id, m.matrix_room_id,
	m.created_at, m.started_at, m.completed_at, m.last_activity_at`

// scanMinion scans all 29 Minion fields from a row in canonical order.
// The SELECT must match minionSelectColumns.
func scanMinion(row Scanner) (*Minion, error) {
	m := &Minion{}
	err := row.Scan(
		&m.ID, &m.UserID, &m.Repo, &m.Task, &m.Model, &m.Status, &m.Platform,
		&m.ClarificationQuestion, &m.ClarificationAnswer, &m.ClarificationMessageID, &m.MatrixClarificationEventID,
		&m.InputTokens, &m.OutputTokens, &m.ReasoningTokens, &m.CacheReadTokens, &m.CacheWriteTokens, &m.CostUSD,
		&m.PRURL, &m.Error, &m.SessionID, &m.PodName,
		&m.DiscordMessageID, &m.DiscordChannelID, &m.MatrixEventID, &m.MatrixRoomID,
		&m.CreatedAt, &m.StartedAt, &m.CompletedAt, &m.LastActivityAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// scanMinionWithOwner scans all 29 Minion fields plus owner IDs (30th, 31st fields).
// Used for queries that JOIN users table for owner validation.
func scanMinionWithOwner(row Scanner) (*MinionWithOwner, error) {
	m := &MinionWithOwner{}
	err := row.Scan(
		&m.ID, &m.UserID, &m.Repo, &m.Task, &m.Model, &m.Status, &m.Platform,
		&m.ClarificationQuestion, &m.ClarificationAnswer, &m.ClarificationMessageID, &m.MatrixClarificationEventID,
		&m.InputTokens, &m.OutputTokens, &m.ReasoningTokens, &m.CacheReadTokens, &m.CacheWriteTokens, &m.CostUSD,
		&m.PRURL, &m.Error, &m.SessionID, &m.PodName,
		&m.DiscordMessageID, &m.DiscordChannelID, &m.MatrixEventID, &m.MatrixRoomID,
		&m.CreatedAt, &m.StartedAt, &m.CompletedAt, &m.LastActivityAt,
		&m.OwnerDiscordID, &m.OwnerMatrixID,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Platform represents the chat platform where a minion was created.
type Platform string

const (
	PlatformDiscord Platform = "discord"
	PlatformMatrix  Platform = "matrix"
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
	ID                         uuid.UUID
	UserID                     uuid.UUID
	Repo                       string
	Task                       string
	Model                      string
	Status                     MinionStatus
	Platform                   Platform
	ClarificationQuestion      *string
	ClarificationAnswer        *string
	ClarificationMessageID     *string // Discord clarification message ID
	MatrixClarificationEventID *string // Matrix clarification event ID
	InputTokens                int64
	OutputTokens               int64
	ReasoningTokens            int64
	CacheReadTokens            int64
	CacheWriteTokens           int64
	CostUSD                    float64
	PRURL                      *string
	Error                      *string
	SessionID                  *string
	PodName                    *string
	DiscordMessageID           *string
	DiscordChannelID           *string
	MatrixEventID              *string // Matrix event ID of original command
	MatrixRoomID               *string // Matrix room ID for notifications
	CreatedAt                  time.Time
	StartedAt                  *time.Time
	CompletedAt                *time.Time
	LastActivityAt             time.Time
}

// CreateMinionParams holds parameters for creating a new minion.
type CreateMinionParams struct {
	UserID                     uuid.UUID
	Repo                       string
	Task                       string
	Model                      string
	Status                     MinionStatus
	Platform                   Platform
	ClarificationQuestion      string
	ClarificationMessageID     string // Discord clarification message ID
	MatrixClarificationEventID string // Matrix clarification event ID
	DiscordMessageID           string
	DiscordChannelID           string
	MatrixEventID              string
	MatrixRoomID               string
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
		ID:       uuid.New(),
		UserID:   params.UserID,
		Repo:     params.Repo,
		Task:     params.Task,
		Model:    params.Model,
		Status:   params.Status,
		Platform: params.Platform,
	}

	if minion.Status == "" {
		minion.Status = StatusPending
	}
	if minion.Platform == "" {
		minion.Platform = PlatformDiscord
	}
	if params.ClarificationQuestion != "" {
		minion.ClarificationQuestion = &params.ClarificationQuestion
	}
	if params.ClarificationMessageID != "" {
		minion.ClarificationMessageID = &params.ClarificationMessageID
	}
	if params.MatrixClarificationEventID != "" {
		minion.MatrixClarificationEventID = &params.MatrixClarificationEventID
	}
	if params.DiscordMessageID != "" {
		minion.DiscordMessageID = &params.DiscordMessageID
	}
	if params.DiscordChannelID != "" {
		minion.DiscordChannelID = &params.DiscordChannelID
	}
	if params.MatrixEventID != "" {
		minion.MatrixEventID = &params.MatrixEventID
	}
	if params.MatrixRoomID != "" {
		minion.MatrixRoomID = &params.MatrixRoomID
	}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO minions (id, user_id, repo, task, model, status, platform,
		 clarification_question, clarification_message_id, matrix_clarification_event_id,
		 discord_message_id, discord_channel_id, matrix_event_id, matrix_room_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING created_at, last_activity_at`,
		minion.ID, minion.UserID, minion.Repo, minion.Task, minion.Model, minion.Status, minion.Platform,
		minion.ClarificationQuestion, minion.ClarificationMessageID, minion.MatrixClarificationEventID,
		minion.DiscordMessageID, minion.DiscordChannelID, minion.MatrixEventID, minion.MatrixRoomID,
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
	row := tx.QueryRow(ctx,
		`SELECT `+minionSelectColumns+`
		 FROM minions 
		 WHERE repo = $1 AND task = $2 AND created_at > NOW() - $3::interval
		 ORDER BY created_at DESC
		 LIMIT 1`,
		repo, task, DuplicateWindow.String(),
	)

	minion, err := scanMinion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return &FindDuplicateResult{Found: false}, nil
	}
	if err != nil {
		return nil, err
	}

	return &FindDuplicateResult{Found: true, ExistingMinion: minion}, nil
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
		ID:       uuid.New(),
		UserID:   params.UserID,
		Repo:     params.Repo,
		Task:     params.Task,
		Model:    params.Model,
		Status:   params.Status,
		Platform: params.Platform,
	}

	if minion.Status == "" {
		minion.Status = StatusPending
	}
	if minion.Platform == "" {
		minion.Platform = PlatformDiscord
	}
	if params.ClarificationQuestion != "" {
		minion.ClarificationQuestion = &params.ClarificationQuestion
	}
	if params.ClarificationMessageID != "" {
		minion.ClarificationMessageID = &params.ClarificationMessageID
	}
	if params.MatrixClarificationEventID != "" {
		minion.MatrixClarificationEventID = &params.MatrixClarificationEventID
	}
	if params.DiscordMessageID != "" {
		minion.DiscordMessageID = &params.DiscordMessageID
	}
	if params.DiscordChannelID != "" {
		minion.DiscordChannelID = &params.DiscordChannelID
	}
	if params.MatrixEventID != "" {
		minion.MatrixEventID = &params.MatrixEventID
	}
	if params.MatrixRoomID != "" {
		minion.MatrixRoomID = &params.MatrixRoomID
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO minions (id, user_id, repo, task, model, status, platform,
		 clarification_question, clarification_message_id, matrix_clarification_event_id,
		 discord_message_id, discord_channel_id, matrix_event_id, matrix_room_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING created_at, last_activity_at`,
		minion.ID, minion.UserID, minion.Repo, minion.Task, minion.Model, minion.Status, minion.Platform,
		minion.ClarificationQuestion, minion.ClarificationMessageID, minion.MatrixClarificationEventID,
		minion.DiscordMessageID, minion.DiscordChannelID, minion.MatrixEventID, minion.MatrixRoomID,
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
	row := s.pool.QueryRow(ctx,
		`SELECT `+minionSelectColumns+`
		 FROM minions WHERE id = $1`,
		id,
	)

	minion, err := scanMinion(row)
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
	Status   *MinionStatus  // optional filter by single status (legacy)
	Statuses []MinionStatus // optional filter by multiple statuses
	Limit    int            // max results, 0 means default (50)
	Offset   int            // pagination offset, 0 means first page
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

	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	// Build query dynamically based on filters
	query := `SELECT ` + minionSelectColumns + ` FROM minions`

	var args []any
	argIdx := 1
	statuses := params.Statuses
	if len(statuses) == 0 && params.Status != nil {
		statuses = []MinionStatus{*params.Status}
	}

	if len(statuses) == 1 {
		query += " WHERE status = $1"
		args = append(args, statuses[0])
		argIdx++
	} else if len(statuses) > 1 {
		statusStrings := make([]string, len(statuses))
		for i, status := range statuses {
			statusStrings[i] = string(status)
		}
		query += " WHERE status = ANY($1)"
		args = append(args, statusStrings)
		argIdx++
	}

	query += " ORDER BY created_at DESC, id DESC"
	query += " LIMIT $" + strconv.Itoa(argIdx)
	args = append(args, limit)
	argIdx++
	query += " OFFSET $" + strconv.Itoa(argIdx)
	args = append(args, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var minions []*Minion
	for rows.Next() {
		m, err := scanMinion(rows)
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

// TerminateResult holds the result of a terminate operation.
type TerminateResult struct {
	// WasTerminated indicates whether the minion was actually terminated
	// (vs already being in a terminal state).
	WasTerminated bool
	// PreviousStatus is the status before termination (for idempotency checks).
	PreviousStatus MinionStatus
	// PodName is the pod name if one was assigned (for k8s cleanup).
	PodName *string
	// Platform indicates which platform to notify.
	Platform Platform
	// DiscordChannelID for sending notifications (Discord).
	DiscordChannelID *string
	// MatrixRoomID for sending notifications (Matrix).
	MatrixRoomID *string
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
	var platform Platform
	var podName, discordChannelID, matrixRoomID *string
	err = tx.QueryRow(ctx,
		`SELECT status, platform, pod_name, discord_channel_id, matrix_room_id FROM minions WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&currentStatus, &platform, &podName, &discordChannelID, &matrixRoomID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	result := &TerminateResult{
		PreviousStatus:   currentStatus,
		PodName:          podName,
		Platform:         platform,
		DiscordChannelID: discordChannelID,
		MatrixRoomID:     matrixRoomID,
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
	// Platform indicates which platform to notify.
	Platform Platform
	// DiscordChannelID for sending notifications (Discord).
	DiscordChannelID *string
	// MatrixRoomID for sending notifications (Matrix).
	MatrixRoomID *string
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
	var platform Platform
	var discordChannelID, matrixRoomID *string
	err = tx.QueryRow(ctx,
		`SELECT status, platform, discord_channel_id, matrix_room_id FROM minions WHERE id = $1 FOR UPDATE`,
		params.ID,
	).Scan(&currentStatus, &platform, &discordChannelID, &matrixRoomID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	result := &CompleteResult{
		PreviousStatus:   currentStatus,
		Platform:         platform,
		DiscordChannelID: discordChannelID,
		MatrixRoomID:     matrixRoomID,
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
	query := `SELECT ` + minionSelectColumns + ` FROM minions WHERE status = ANY($1)`

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
		m, err := scanMinion(rows)
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
	query := `SELECT ` + minionSelectColumns + `
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
		m, err := scanMinion(rows)
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
	ID               uuid.UUID
	InputTokens      int64
	OutputTokens     int64
	ReasoningTokens  int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
}

// UpdateTokenUsage atomically adds to a minion's token usage counters.
// Also updates last_activity_at to track pod activity.
func (s *MinionStore) UpdateTokenUsage(ctx context.Context, params UpdateTokenUsageParams) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET 
			input_tokens = input_tokens + $1,
			output_tokens = output_tokens + $2,
			reasoning_tokens = reasoning_tokens + $3,
			cache_read_tokens = cache_read_tokens + $4,
			cache_write_tokens = cache_write_tokens + $5,
			cost_usd = cost_usd + $6,
			last_activity_at = NOW()
		WHERE id = $7`,
		params.InputTokens, params.OutputTokens, params.ReasoningTokens,
		params.CacheReadTokens, params.CacheWriteTokens, params.CostUSD, params.ID,
	)
	return err
}

// ListIdleRunning returns running minions with last_activity_at older than threshold.
// Used by watchdog to detect idle minions that may be stuck.
func (s *MinionStore) ListIdleRunning(ctx context.Context, idleThreshold time.Duration) ([]*Minion, error) {
	query := `SELECT ` + minionSelectColumns + `
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
		m, err := scanMinion(rows)
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
	query := `SELECT ` + minionSelectColumns + `
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
		m, err := scanMinion(rows)
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

// MinionWithOwner extends Minion with owner's IDs from the joined users table.
// Used for clarification reply validation.
type MinionWithOwner struct {
	Minion
	OwnerDiscordID string
	OwnerMatrixID  *string
}

// GetByClarificationMessageID looks up a minion by its Discord clarification message ID.
// JOINs the users table to include the owner's Discord ID for reply validation.
// Used for processing replies to clarification questions.
func (s *MinionStore) GetByClarificationMessageID(ctx context.Context, messageID string) (*MinionWithOwner, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT m.id, m.user_id, m.repo, m.task, m.model, m.status, m.platform,
		        m.clarification_question, m.clarification_answer, m.clarification_message_id, m.matrix_clarification_event_id,
		        m.input_tokens, m.output_tokens, m.reasoning_tokens, m.cache_read_tokens, m.cache_write_tokens, m.cost_usd,
		        m.pr_url, m.error, m.session_id, m.pod_name,
		        m.discord_message_id, m.discord_channel_id, m.matrix_event_id, m.matrix_room_id,
		        m.created_at, m.started_at, m.completed_at, m.last_activity_at,
		        u.discord_id, u.matrix_id
		 FROM minions m
		 JOIN users u ON m.user_id = u.id
		 WHERE m.clarification_message_id = $1`,
		messageID,
	)

	m, err := scanMinionWithOwner(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return m, nil
}

// GetByMatrixClarificationEventID looks up a minion by its Matrix clarification event ID.
// JOINs the users table to include the owner's Matrix ID for reply validation.
// Used for processing replies to clarification questions in Matrix.
func (s *MinionStore) GetByMatrixClarificationEventID(ctx context.Context, eventID string) (*MinionWithOwner, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+minionSelectColumnsWithPrefix+`,
		        u.discord_id, u.matrix_id
		 FROM minions m
		 JOIN users u ON m.user_id = u.id
		 WHERE m.matrix_clarification_event_id = $1`,
		eventID,
	)

	m, err := scanMinionWithOwner(row)
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

// ListTerminalWithPodOlderThan returns terminal minions that still have pod_name set
// and whose terminal timestamp is older than the provided age threshold.
//
// Primary timestamp is completed_at. For legacy rows where completed_at is NULL,
// it falls back to last_activity_at and then created_at so stale terminal pods are
// still eligible for cleanup.
// Used by watchdog to perform delayed pod cleanup.
func (s *MinionStore) ListTerminalWithPodOlderThan(ctx context.Context, age time.Duration) ([]*Minion, error) {
	query := `SELECT ` + minionSelectColumns + `
	 FROM minions
	 WHERE status IN ('completed', 'failed', 'terminated')
	   AND pod_name IS NOT NULL
	   AND COALESCE(completed_at, last_activity_at, created_at) < NOW() - $1::interval`

	rows, err := s.pool.Query(ctx, query, age.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var minions []*Minion
	for rows.Next() {
		m, err := scanMinion(rows)
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

// ClearPodName clears pod_name for a minion after pod cleanup.
// This prevents repeated cleanup attempts for already-deleted pods.
func (s *MinionStore) ClearPodName(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE minions SET pod_name = NULL, last_activity_at = NOW() WHERE id = $1`,
		id,
	)
	return err
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

	// Get totals - combine input+cache_read, output+reasoning+cache_write
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0), 
		        COALESCE(SUM(input_tokens + cache_read_tokens), 0), 
		        COALESCE(SUM(output_tokens + reasoning_tokens + cache_write_tokens), 0)
		 FROM minions`,
	).Scan(&stats.TotalCostUSD, &stats.TotalInputTokens, &stats.TotalOutputTokens)
	if err != nil {
		return nil, err
	}

	// Get breakdown by model - combine token fields in aggregation
	rows, err := s.pool.Query(ctx,
		`SELECT model, 
		        COALESCE(SUM(cost_usd), 0), 
		        COALESCE(SUM(input_tokens + cache_read_tokens), 0), 
		        COALESCE(SUM(output_tokens + reasoning_tokens + cache_write_tokens), 0), 
		        COUNT(*)
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
