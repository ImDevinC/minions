package streaming

import (
	"context"
	"log/slog"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/google/uuid"
)

// DBEventHandler persists events to the database and updates token usage.
type DBEventHandler struct {
	eventStore  *db.EventStore
	minionStore *db.MinionStore
	logger      *slog.Logger
	// hub is optional - for WebSocket broadcasting (streaming-2)
	// hub *Hub
}

// DBEventHandlerConfig holds configuration for DBEventHandler.
type DBEventHandlerConfig struct {
	EventStore  *db.EventStore
	MinionStore *db.MinionStore
	Logger      *slog.Logger
}

// NewDBEventHandler creates a new database-backed event handler.
func NewDBEventHandler(config DBEventHandlerConfig) *DBEventHandler {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &DBEventHandler{
		eventStore:  config.EventStore,
		minionStore: config.MinionStore,
		logger:      config.Logger,
	}
}

// HandleEvent persists the event to minion_events table.
func (h *DBEventHandler) HandleEvent(ctx context.Context, minionID uuid.UUID, event *PodEvent) error {
	dbEvent := &db.MinionEvent{
		MinionID:  minionID,
		EventType: event.Type,
		Content:   event.Content,
	}

	if err := h.eventStore.InsertEvent(ctx, dbEvent); err != nil {
		h.logger.Error("failed to persist event",
			"minion_id", minionID,
			"event_type", event.Type,
			"error", err,
		)
		return err
	}

	h.logger.Debug("persisted event",
		"minion_id", minionID,
		"event_type", event.Type,
		"event_id", dbEvent.ID,
	)

	// TODO: broadcast to WebSocket hub when streaming-2 is implemented
	// if h.hub != nil {
	//     h.hub.Broadcast(minionID, event)
	// }

	return nil
}

// HandleTokenUsage accumulates token usage in the minion record.
func (h *DBEventHandler) HandleTokenUsage(ctx context.Context, minionID uuid.UUID, usage TokenUsage) error {
	params := db.UpdateTokenUsageParams{
		ID:           minionID,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}

	if err := h.minionStore.UpdateTokenUsage(ctx, params); err != nil {
		h.logger.Error("failed to update token usage",
			"minion_id", minionID,
			"input_tokens", usage.InputTokens,
			"output_tokens", usage.OutputTokens,
			"error", err,
		)
		return err
	}

	h.logger.Debug("updated token usage",
		"minion_id", minionID,
		"input_tokens", usage.InputTokens,
		"output_tokens", usage.OutputTokens,
	)

	return nil
}

// HandleDisconnect logs the disconnection. Could also update minion status or notify.
func (h *DBEventHandler) HandleDisconnect(ctx context.Context, minionID uuid.UUID, err error) {
	h.logger.Warn("SSE connection lost",
		"minion_id", minionID,
		"error", err,
	)
	// Don't update status here - the client will reconnect.
	// Status updates should come from callbacks or watchdog.
}
