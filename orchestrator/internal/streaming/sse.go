// Package streaming provides SSE client and WebSocket server for pod event streaming.
package streaming

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSE reconnection configuration.
const (
	// InitialReconnectDelay is the initial backoff before first reconnection attempt.
	InitialReconnectDelay = 1 * time.Second
	// MaxReconnectDelay caps the backoff duration.
	MaxReconnectDelay = 30 * time.Second
	// ReconnectBackoffMultiplier is the factor by which backoff increases each retry.
	ReconnectBackoffMultiplier = 2
	// SSEReadTimeout is the timeout for reading a single SSE event.
	SSEReadTimeout = 60 * time.Second
)

// ErrPodTerminated indicates the pod is no longer available.
var ErrPodTerminated = errors.New("pod terminated or unreachable")

// PodEvent represents an event received from a devbox pod's /events SSE endpoint.
type PodEvent struct {
	Type    string         `json:"type"`              // event type (e.g., "message", "tool_use", "token_usage")
	Content map[string]any `json:"content,omitempty"` // event-specific payload
}

// TokenUsage represents token usage data extracted from events.
type TokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// EventHandler processes events received from the SSE stream.
// Implementations persist events to the database and broadcast to WebSocket clients.
type EventHandler interface {
	// HandleEvent processes a single event from the pod.
	HandleEvent(ctx context.Context, minionID uuid.UUID, event *PodEvent) error
	// HandleTokenUsage accumulates token usage for a minion.
	HandleTokenUsage(ctx context.Context, minionID uuid.UUID, usage TokenUsage) error
	// HandleDisconnect is called when the SSE connection is lost.
	HandleDisconnect(ctx context.Context, minionID uuid.UUID, err error)
}

// PodIPProvider resolves pod IPs for connecting to SSE endpoints.
type PodIPProvider interface {
	// GetPodIP returns the IP address of a pod by name.
	GetPodIP(ctx context.Context, podName string) (string, error)
}

// SSEClientConfig holds configuration for the SSE client.
type SSEClientConfig struct {
	// PodPort is the port where devbox serves the /events endpoint.
	PodPort int
	// Logger for structured logging.
	Logger *slog.Logger
}

// connection tracks an active SSE connection for a minion.
type connection struct {
	cancel  context.CancelFunc
	podName string
}

// SSEClient connects to pod SSE endpoints and handles events.
type SSEClient struct {
	podIPProvider PodIPProvider
	handler       EventHandler
	httpClient    *http.Client
	config        SSEClientConfig

	// Track active connections for cleanup
	mu          sync.Mutex
	connections map[uuid.UUID]*connection
}

// NewSSEClient creates a new SSE client.
func NewSSEClient(podIPProvider PodIPProvider, handler EventHandler, config SSEClientConfig) *SSEClient {
	if config.PodPort == 0 {
		config.PodPort = 4096 // default opencode serve port
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &SSEClient{
		podIPProvider: podIPProvider,
		handler:       handler,
		httpClient: &http.Client{
			Timeout: 0, // no timeout for SSE (long-lived connection)
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxConnsPerHost:     50,
				MaxIdleConnsPerHost: 50,
			},
		},
		config:      config,
		connections: make(map[uuid.UUID]*connection),
	}
}

// Connect starts streaming events from a pod.
// This runs in a goroutine and reconnects automatically on disconnection.
// Call Disconnect to stop streaming.
func (c *SSEClient) Connect(ctx context.Context, minionID uuid.UUID, podName string) {
	// Create a cancellable context for this connection
	connCtx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	// Cancel any existing connection for this minion
	if existingConn, ok := c.connections[minionID]; ok {
		existingConn.cancel()
	}
	c.connections[minionID] = &connection{
		cancel:  cancel,
		podName: podName,
	}
	c.mu.Unlock()

	go c.connectWithRetry(connCtx, minionID, podName)
}

// Disconnect stops streaming events for a minion.
func (c *SSEClient) Disconnect(minionID uuid.UUID) {
	c.mu.Lock()
	if conn, ok := c.connections[minionID]; ok {
		conn.cancel()
		delete(c.connections, minionID)
	}
	c.mu.Unlock()
}

// connectWithRetry handles the reconnection loop with exponential backoff.
func (c *SSEClient) connectWithRetry(ctx context.Context, minionID uuid.UUID, podName string) {
	backoff := InitialReconnectDelay

	for {
		select {
		case <-ctx.Done():
			c.config.Logger.Info("SSE connection cancelled",
				"minion_id", minionID,
				"pod_name", podName,
			)
			return
		default:
		}

		err := c.streamEvents(ctx, minionID, podName)
		if err == nil {
			// Normal termination (context cancelled or pod completed)
			return
		}

		if errors.Is(err, context.Canceled) {
			return
		}

		c.config.Logger.Warn("SSE connection lost, will reconnect",
			"minion_id", minionID,
			"pod_name", podName,
			"error", err,
			"backoff", backoff,
		)

		// Notify handler of disconnection
		c.handler.HandleDisconnect(ctx, minionID, err)

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff with cap
		backoff *= ReconnectBackoffMultiplier
		if backoff > MaxReconnectDelay {
			backoff = MaxReconnectDelay
		}
	}
}

// streamEvents connects to the pod and streams events until disconnection or error.
func (c *SSEClient) streamEvents(ctx context.Context, minionID uuid.UUID, podName string) error {
	// Get pod IP
	podIP, err := c.podIPProvider.GetPodIP(ctx, podName)
	if err != nil {
		return fmt.Errorf("failed to get pod IP: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d/event", podIP, c.config.PodPort)
	c.config.Logger.Info("connecting to pod SSE endpoint",
		"minion_id", minionID,
		"pod_name", podName,
		"url", url,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	c.config.Logger.Info("SSE connection established",
		"minion_id", minionID,
		"pod_name", podName,
	)

	// Reset backoff on successful connection (happens in caller after successful return)
	return c.readEventStream(ctx, minionID, resp.Body)
}

// readEventStream reads and processes events from an SSE stream.
func (c *SSEClient) readEventStream(ctx context.Context, minionID uuid.UUID, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)

	var eventType string
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				if err := c.processEvent(ctx, minionID, eventType, data); err != nil {
					c.config.Logger.Error("failed to process event",
						"minion_id", minionID,
						"event_type", eventType,
						"error", err,
					)
					// Continue processing other events; don't fail the stream
				}
			}
			eventType = ""
			dataLines = nil
			continue
		}

		// Parse SSE fields
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		}
		// Ignore id:, retry:, and comments (lines starting with :)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream read error: %w", err)
	}

	// Stream ended normally (EOF)
	return nil
}

// processEvent parses and handles a single SSE event.
func (c *SSEClient) processEvent(ctx context.Context, minionID uuid.UUID, eventType, data string) error {
	var event PodEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		// Try to salvage if JSON is invalid - create a raw content event
		event = PodEvent{
			Type: eventType,
			Content: map[string]any{
				"raw": data,
			},
		}
	}

	// Use event type from SSE field if not in JSON
	if event.Type == "" {
		event.Type = eventType
	}
	if event.Type == "" {
		event.Type = "unknown"
	}

	// Extract and accumulate token usage if present
	if usage, ok := extractTokenUsage(&event); ok {
		if err := c.handler.HandleTokenUsage(ctx, minionID, usage); err != nil {
			c.config.Logger.Error("failed to update token usage",
				"minion_id", minionID,
				"error", err,
			)
		}
	}

	// Persist and broadcast event
	return c.handler.HandleEvent(ctx, minionID, &event)
}

// extractTokenUsage attempts to extract token usage from an event.
// Returns false if no token usage data is present.
func extractTokenUsage(event *PodEvent) (TokenUsage, bool) {
	if event.Content == nil {
		return TokenUsage{}, false
	}

	// Look for token_usage or usage field in content
	var usageData map[string]any
	if u, ok := event.Content["token_usage"].(map[string]any); ok {
		usageData = u
	} else if u, ok := event.Content["usage"].(map[string]any); ok {
		usageData = u
	} else if event.Type == "token_usage" {
		usageData = event.Content
	}

	if usageData == nil {
		return TokenUsage{}, false
	}

	var usage TokenUsage
	if input, ok := usageData["input_tokens"].(float64); ok {
		usage.InputTokens = int64(input)
	}
	if output, ok := usageData["output_tokens"].(float64); ok {
		usage.OutputTokens = int64(output)
	}

	// Only return true if we actually got some token data
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		return usage, true
	}

	return TokenUsage{}, false
}

// NoOpEventHandler is a stub implementation for testing.
type NoOpEventHandler struct {
	Logger *slog.Logger
}

// HandleEvent logs the event but does nothing else.
func (h *NoOpEventHandler) HandleEvent(ctx context.Context, minionID uuid.UUID, event *PodEvent) error {
	if h.Logger != nil {
		h.Logger.Debug("no-op event handler",
			"minion_id", minionID,
			"event_type", event.Type,
		)
	}
	return nil
}

// HandleTokenUsage logs the usage but does nothing else.
func (h *NoOpEventHandler) HandleTokenUsage(ctx context.Context, minionID uuid.UUID, usage TokenUsage) error {
	if h.Logger != nil {
		h.Logger.Debug("no-op token usage handler",
			"minion_id", minionID,
			"input_tokens", usage.InputTokens,
			"output_tokens", usage.OutputTokens,
		)
	}
	return nil
}

// HandleDisconnect logs the disconnect but does nothing else.
func (h *NoOpEventHandler) HandleDisconnect(ctx context.Context, minionID uuid.UUID, err error) {
	if h.Logger != nil {
		h.Logger.Debug("no-op disconnect handler",
			"minion_id", minionID,
			"error", err,
		)
	}
}
