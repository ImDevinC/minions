// Package db provides database connectivity and repositories.
package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MinionEvent represents a single event in a minion's event log.
type MinionEvent struct {
	ID        uuid.UUID
	MinionID  uuid.UUID
	Timestamp time.Time
	EventType string
	Content   map[string]any // JSONB content
}

// EventStore handles minion event database operations.
type EventStore struct {
	pool *pgxpool.Pool
}

// NewEventStore creates a new EventStore.
func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

const defaultEventLimit = 100

// InsertEvent persists a new event to the minion_events table.
func (s *EventStore) InsertEvent(ctx context.Context, event *MinionEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO minion_events (id, minion_id, timestamp, event_type, content)
		 VALUES ($1, $2, $3, $4, $5)`,
		event.ID, event.MinionID, event.Timestamp, event.EventType, event.Content,
	)
	return err
}

// GetRecentEvents retrieves the most recent events for a minion.
// Returns events ordered by timestamp DESC (newest first).
func (s *EventStore) GetRecentEvents(ctx context.Context, minionID uuid.UUID, limit int) ([]*MinionEvent, error) {
	if limit <= 0 {
		limit = defaultEventLimit
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, minion_id, timestamp, event_type, content
		 FROM minion_events
		 WHERE minion_id = $1
		 ORDER BY timestamp DESC
		 LIMIT $2`,
		minionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*MinionEvent
	for rows.Next() {
		e := &MinionEvent{}
		err := rows.Scan(&e.ID, &e.MinionID, &e.Timestamp, &e.EventType, &e.Content)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// GetEventsSince retrieves events for a minion with timestamp > since.
// Returns events ordered by timestamp ASC (oldest first, for appending to existing log).
// Useful for reconnection after WebSocket disconnect to fetch missed events.
func (s *EventStore) GetEventsSince(ctx context.Context, minionID uuid.UUID, since time.Time, limit int) ([]*MinionEvent, error) {
	if limit <= 0 {
		limit = 1000 // Higher limit for catch-up, capped to prevent runaway queries
	}
	if limit > 10000 {
		limit = 10000
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, minion_id, timestamp, event_type, content
		 FROM minion_events
		 WHERE minion_id = $1 AND timestamp > $2
		 ORDER BY timestamp ASC
		 LIMIT $3`,
		minionID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*MinionEvent
	for rows.Next() {
		e := &MinionEvent{}
		err := rows.Scan(&e.ID, &e.MinionID, &e.Timestamp, &e.EventType, &e.Content)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// GetLastAssistantMessage returns the most recent assistant message text
// for a minion. This extracts the final summary from message.updated events
// where the assistant role is present with text content.
//
// The message structure is: content.parts[].text where parts[].type == "text"
// Returns empty string if no assistant message is found.
func (s *EventStore) GetLastAssistantMessage(ctx context.Context, minionID uuid.UUID) (string, error) {
	// Find the most recent message.updated event with role=assistant
	// The structure is: content -> role, parts
	// We want the text from parts where type=text
	var content map[string]any
	err := s.pool.QueryRow(ctx,
		`SELECT content
		 FROM minion_events
		 WHERE minion_id = $1 
		   AND event_type = 'message.updated'
		   AND content->>'role' = 'assistant'
		 ORDER BY timestamp DESC
		 LIMIT 1`,
		minionID,
	).Scan(&content)

	if err != nil {
		// pgx returns no rows error when nothing found
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}

	if content == nil {
		return "", nil
	}

	// Extract text from parts array where type=text
	// Structure: {"role": "assistant", "parts": [{"type": "text", "text": "..."}]}
	parts, ok := content["parts"].([]any)
	if !ok {
		return "", nil
	}

	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] == "text" {
			if text, ok := part["text"].(string); ok && text != "" {
				return text, nil
			}
		}
	}

	return "", nil
}

// GetLatestSessionError returns the error message from the most recent
// session.error event for a minion, if any exists. This is used to detect
// cases where OpenCode failed (e.g., model not found) but the session still
// transitioned to idle, causing devbox to incorrectly report success.
//
// Returns empty string if no session.error events exist.
func (s *EventStore) GetLatestSessionError(ctx context.Context, minionID uuid.UUID) (string, error) {
	// Extract error message from JSONB using PostgreSQL JSON operators.
	// Structure: {"error": {"data": {"message": "..."}, "name": "..."}, "sessionID": "..."}
	// Try content->'error'->'data'->>'message' first, fallback to content->'error'->>'name'.
	var errMsg *string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(
			content->'error'->'data'->>'message',
			content->'error'->>'name'
		)
		FROM minion_events
		WHERE minion_id = $1 AND event_type = 'session.error'
		ORDER BY timestamp DESC
		LIMIT 1`,
		minionID,
	).Scan(&errMsg)

	if err != nil {
		// pgx returns no rows error when nothing found
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}

	if errMsg == nil {
		return "", nil
	}
	return *errMsg, nil
}
