package streaming

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WebSocket configuration.
const (
	// WriteWait is the time allowed to write a message to the client.
	WriteWait = 10 * time.Second
	// PongWait is the time allowed to read the next pong message from the client.
	PongWait = 60 * time.Second
	// PingPeriod is how often to send pings. Must be less than PongWait.
	PingPeriod = (PongWait * 9) / 10
	// MaxMessageSize is the maximum message size allowed from client.
	MaxMessageSize = 512
)

// Client represents a WebSocket client connection subscribed to a minion's events.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	minionID uuid.UUID
	send     chan []byte
}

// Hub manages WebSocket clients and broadcasts events to them.
// It's a pub/sub system where clients subscribe to specific minion IDs.
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                           Hub                               │
//	│  ┌─────────────────────────────────────────────────────┐   │
//	│  │  clients: map[minionID] -> set of Client            │   │
//	│  └─────────────────────────────────────────────────────┘   │
//	│                                                             │
//	│   register ←───── new WS connection                        │
//	│   unregister ←─── WS closed                                │
//	│   broadcast ←──── SSE event received                       │
//	└─────────────────────────────────────────────────────────────┘
type Hub struct {
	// clients holds subscribed clients per minion ID.
	clients map[uuid.UUID]map[*Client]struct{}

	// register channel for adding new clients.
	register chan *Client

	// unregister channel for removing clients.
	unregister chan *Client

	// broadcast channel for sending events to clients.
	broadcast chan broadcastMessage

	// mu protects clients map for concurrent access.
	mu sync.RWMutex

	// logger for structured logging.
	logger *slog.Logger

	// done signals shutdown.
	done chan struct{}
}

// broadcastMessage holds an event to broadcast to all clients of a minion.
type broadcastMessage struct {
	minionID uuid.UUID
	data     []byte
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		clients:    make(map[uuid.UUID]map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan broadcastMessage, 256), // buffered to prevent blocking SSE handler
		logger:     logger,
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main loop. Call in a goroutine.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.shutdown()
			return
		case <-h.done:
			return
		case client := <-h.register:
			h.addClient(client)
		case client := <-h.unregister:
			h.removeClient(client)
		case msg := <-h.broadcast:
			h.broadcastToMinion(msg.minionID, msg.data)
		}
	}
}

// Stop gracefully shuts down the hub.
func (h *Hub) Stop() {
	close(h.done)
}

// Register adds a client to the hub.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// Broadcast sends an event to all clients subscribed to a minion.
func (h *Hub) Broadcast(minionID uuid.UUID, event *PodEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("failed to marshal event for broadcast",
			"minion_id", minionID,
			"error", err,
		)
		return
	}

	select {
	case h.broadcast <- broadcastMessage{minionID: minionID, data: data}:
	default:
		// Channel full, drop the message. This shouldn't happen with the buffer size,
		// but better to drop than block the SSE handler.
		h.logger.Warn("broadcast channel full, dropping event",
			"minion_id", minionID,
			"event_type", event.Type,
		)
	}
}

// ClientCount returns the number of clients subscribed to a minion.
func (h *Hub) ClientCount(minionID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.clients[minionID]; ok {
		return len(clients)
	}
	return 0
}

// addClient registers a client with the hub.
func (h *Hub) addClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.minionID]; !ok {
		h.clients[client.minionID] = make(map[*Client]struct{})
	}
	h.clients[client.minionID][client] = struct{}{}

	h.logger.Debug("client registered",
		"minion_id", client.minionID,
		"client_count", len(h.clients[client.minionID]),
	)
}

// removeClient unregisters a client and closes its send channel.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.clients[client.minionID]; ok {
		if _, exists := clients[client]; exists {
			delete(clients, client)
			close(client.send)

			// Clean up empty maps
			if len(clients) == 0 {
				delete(h.clients, client.minionID)
			}

			h.logger.Debug("client unregistered",
				"minion_id", client.minionID,
				"remaining_clients", len(h.clients[client.minionID]),
			)
		}
	}
}

// broadcastToMinion sends data to all clients subscribed to a minion.
func (h *Hub) broadcastToMinion(minionID uuid.UUID, data []byte) {
	h.mu.RLock()
	clients, ok := h.clients[minionID]
	if !ok || len(clients) == 0 {
		h.mu.RUnlock()
		return
	}

	// Copy clients slice to avoid holding lock during send
	clientList := make([]*Client, 0, len(clients))
	for client := range clients {
		clientList = append(clientList, client)
	}
	h.mu.RUnlock()

	for _, client := range clientList {
		select {
		case client.send <- data:
		default:
			// Client's send buffer is full, it's probably dead. Remove it.
			h.Unregister(client)
		}
	}
}

// shutdown closes all client connections.
func (h *Hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, clients := range h.clients {
		for client := range clients {
			close(client.send)
		}
	}
	h.clients = make(map[uuid.UUID]map[*Client]struct{})
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn, minionID uuid.UUID) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		minionID: minionID,
		send:     make(chan []byte, 256),
	}
}

// ReadPump pumps messages from the WebSocket connection.
// We don't expect clients to send data, but we need this to handle pongs.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(MaxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(PongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(PongWait))
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Debug("websocket read error",
					"minion_id", c.minionID,
					"error", err,
				)
			}
			break
		}
		// We don't process incoming messages; clients are receive-only
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(WriteWait)); err != nil {
				return
			}
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
