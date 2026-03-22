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
