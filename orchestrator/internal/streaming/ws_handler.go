package streaming

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// upgrader configures the WebSocket upgrade.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins in dev; tighten this in production via CheckOrigin
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// StreamHandler handles WebSocket connections for live event streaming.
type StreamHandler struct {
	hub    *Hub
	logger *slog.Logger
}

// StreamHandlerConfig holds configuration for StreamHandler.
type StreamHandlerConfig struct {
	Hub    *Hub
	Logger *slog.Logger
}

// NewStreamHandler creates a new WebSocket stream handler.
func NewStreamHandler(cfg StreamHandlerConfig) *StreamHandler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &StreamHandler{
		hub:    cfg.Hub,
		logger: cfg.Logger,
	}
}

// HandleStream handles WebSocket connections at /api/minions/:id/stream.
// It upgrades the HTTP connection to WebSocket and registers the client with the hub.
// The client will receive all events broadcast for the specified minion.
func (h *StreamHandler) HandleStream(w http.ResponseWriter, r *http.Request, minionID uuid.UUID) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade connection",
			"minion_id", minionID,
			"error", err,
		)
		return
	}

	client := NewClient(h.hub, conn, minionID)
	h.hub.Register(client)

	h.logger.Info("websocket client connected",
		"minion_id", minionID,
	)

	// Start the client pumps in goroutines
	go client.WritePump()
	go client.ReadPump()
}

// Hub returns the underlying hub (for testing or external access).
func (h *StreamHandler) Hub() *Hub {
	return h.hub
}
