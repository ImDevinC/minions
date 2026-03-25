package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockPodIPProvider returns a fixed IP for testing.
type mockPodIPProvider struct {
	ip  string
	err error
}

func (m *mockPodIPProvider) GetPodIP(ctx context.Context, podName string) (string, error) {
	return m.ip, m.err
}

// mockEventHandler records events for verification.
type mockEventHandler struct {
	mu           sync.Mutex
	events       []*PodEvent
	tokenUsages  []TokenUsage
	disconnects  int
	disconnectCh chan struct{}
}

func newMockEventHandler() *mockEventHandler {
	return &mockEventHandler{
		disconnectCh: make(chan struct{}, 10),
	}
}

func (h *mockEventHandler) HandleEvent(ctx context.Context, minionID uuid.UUID, event *PodEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	return nil
}

func (h *mockEventHandler) HandleTokenUsage(ctx context.Context, minionID uuid.UUID, usage TokenUsage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokenUsages = append(h.tokenUsages, usage)
	return nil
}

func (h *mockEventHandler) HandleDisconnect(ctx context.Context, minionID uuid.UUID, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnects++
	select {
	case h.disconnectCh <- struct{}{}:
	default:
	}
}

func (h *mockEventHandler) getEvents() []*PodEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]*PodEvent, len(h.events))
	copy(result, h.events)
	return result
}

func (h *mockEventHandler) getTokenUsages() []TokenUsage {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]TokenUsage, len(h.tokenUsages))
	copy(result, h.tokenUsages)
	return result
}

func TestSSEClient_BasicEventStream(t *testing.T) {
	// Start a test SSE server
	eventsSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter doesn't support Flush")
		}

		// Send a few events
		events := []string{
			"event: message\ndata: {\"type\":\"message\",\"content\":{\"text\":\"hello\"}}\n\n",
			"event: tool_use\ndata: {\"type\":\"tool_use\",\"content\":{\"tool\":\"read_file\"}}\n\n",
			"event: token_usage\ndata: {\"type\":\"token_usage\",\"content\":{\"input_tokens\":100,\"output_tokens\":50}}\n\n",
		}

		for _, event := range events {
			fmt.Fprint(w, event)
			flusher.Flush()
		}

		close(eventsSent)

		// Keep connection open briefly to let client process
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// Extract host:port from test server URL
	serverAddr := server.Listener.Addr().String()

	handler := newMockEventHandler()
	provider := &mockPodIPProvider{ip: serverAddr}

	// Note: test server doesn't use port 4096, so we need to extract the actual port
	// The server URL is like http://127.0.0.1:xxxxx, so we pass the full addr
	client := NewSSEClient(provider, handler, SSEClientConfig{
		PodPort: 0, // Will be ignored since we use the full URL approach below
	})

	// Override the URL construction for testing
	// We need a different approach - let's create a custom client that connects to our test server

	minionID := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Manually stream events for testing (bypassing Connect which does goroutine)
	err := client.streamEventsTestURL(ctx, minionID, server.URL+"/events")
	// Stream will end when server closes connection
	if err != nil {
		t.Logf("stream ended with: %v", err)
	}

	// Wait for events to be sent
	<-eventsSent

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify events received
	events := handler.getEvents()
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	// Verify token usage extracted
	usages := handler.getTokenUsages()
	if len(usages) != 1 {
		t.Errorf("expected 1 token usage, got %d", len(usages))
	} else {
		if usages[0].InputTokens != 100 {
			t.Errorf("expected input_tokens=100, got %d", usages[0].InputTokens)
		}
		if usages[0].OutputTokens != 50 {
			t.Errorf("expected output_tokens=50, got %d", usages[0].OutputTokens)
		}
	}
}

// streamEventsTestURL is a test helper that connects to a specific URL.
func (c *SSEClient) streamEventsTestURL(ctx context.Context, minionID uuid.UUID, url string) error {
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

	return c.readEventStream(ctx, minionID, resp.Body)
}

func TestExtractTokenUsage(t *testing.T) {
	tests := []struct {
		name      string
		event     PodEvent
		wantUsage TokenUsage
		wantOK    bool
	}{
		{
			name: "token_usage event type",
			event: PodEvent{
				Type: "token_usage",
				Content: map[string]any{
					"input_tokens":  float64(100),
					"output_tokens": float64(50),
				},
			},
			wantUsage: TokenUsage{InputTokens: 100, OutputTokens: 50},
			wantOK:    true,
		},
		{
			name: "usage in content",
			event: PodEvent{
				Type: "message",
				Content: map[string]any{
					"usage": map[string]any{
						"input_tokens":  float64(200),
						"output_tokens": float64(100),
					},
				},
			},
			wantUsage: TokenUsage{InputTokens: 200, OutputTokens: 100},
			wantOK:    true,
		},
		{
			name: "token_usage in content",
			event: PodEvent{
				Type: "response",
				Content: map[string]any{
					"token_usage": map[string]any{
						"input_tokens":  float64(300),
						"output_tokens": float64(150),
					},
				},
			},
			wantUsage: TokenUsage{InputTokens: 300, OutputTokens: 150},
			wantOK:    true,
		},
		{
			name: "message.updated with full token details",
			event: PodEvent{
				Type: "message.updated",
				Content: map[string]any{
					"info": map[string]any{
						"cost": 0.02954525,
						"tokens": map[string]any{
							"input":     float64(1763),
							"output":    float64(929),
							"reasoning": float64(793),
							"cache": map[string]any{
								"read":  float64(13440),
								"write": float64(0),
							},
						},
					},
				},
			},
			wantUsage: TokenUsage{
				InputTokens:      1763,
				OutputTokens:     929,
				ReasoningTokens:  793,
				CacheReadTokens:  13440,
				CacheWriteTokens: 0,
				CostUSD:          0.02954525,
			},
			wantOK: true,
		},
		{
			name: "message.updated with partial tokens",
			event: PodEvent{
				Type: "message.updated",
				Content: map[string]any{
					"info": map[string]any{
						"cost": 0.001,
						"tokens": map[string]any{
							"input":  float64(100),
							"output": float64(50),
						},
					},
				},
			},
			wantUsage: TokenUsage{
				InputTokens:  100,
				OutputTokens: 50,
				CostUSD:      0.001,
			},
			wantOK: true,
		},
		{
			name: "message.updated with missing cost",
			event: PodEvent{
				Type: "message.updated",
				Content: map[string]any{
					"info": map[string]any{
						"tokens": map[string]any{
							"input":  float64(100),
							"output": float64(50),
						},
					},
				},
			},
			wantUsage: TokenUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
			wantOK: true,
		},
		{
			name: "no token usage",
			event: PodEvent{
				Type: "message",
				Content: map[string]any{
					"text": "hello",
				},
			},
			wantUsage: TokenUsage{},
			wantOK:    false,
		},
		{
			name: "nil content",
			event: PodEvent{
				Type: "ping",
			},
			wantUsage: TokenUsage{},
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUsage, gotOK := extractTokenUsage(&tt.event)
			if gotOK != tt.wantOK {
				t.Errorf("extractTokenUsage() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotUsage != tt.wantUsage {
				t.Errorf("extractTokenUsage() = %+v, want %+v", gotUsage, tt.wantUsage)
			}
		})
	}
}

func TestSSEClient_OpenCodePropertiesFormat(t *testing.T) {
	// Test that OpenCode's {type, properties} format is normalized to {type, content}
	eventsSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter doesn't support Flush")
		}

		// Send events in OpenCode format: {type, properties} instead of {type, content}
		events := []string{
			"data: {\"type\":\"message.part.delta\",\"properties\":{\"sessionID\":\"sess-1\",\"messageID\":\"msg-1\",\"partID\":\"part-1\",\"delta\":\"Hello\"}}\n\n",
			"data: {\"type\":\"message.part.updated\",\"properties\":{\"sessionID\":\"sess-1\",\"messageID\":\"msg-1\",\"partID\":\"part-1\",\"type\":\"text\",\"text\":\"Hello world\"}}\n\n",
			"data: {\"type\":\"session.status\",\"properties\":{\"status\":\"running\"}}\n\n",
		}

		for _, event := range events {
			fmt.Fprint(w, event)
			flusher.Flush()
		}
		close(eventsSent)

		// Keep connection open briefly for client to process
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	handler := newMockEventHandler()
	provider := &mockPodIPProvider{ip: "127.0.0.1"}

	client := NewSSEClient(provider, handler, SSEClientConfig{
		PodPort: 4096,
	})

	minionID := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use test helper to connect to specific URL
	err := client.streamEventsTestURL(ctx, minionID, server.URL+"/event")
	// Stream will end when server closes connection
	if err != nil {
		t.Logf("stream ended with: %v", err)
	}

	<-eventsSent

	// Wait a bit for events to be processed
	time.Sleep(50 * time.Millisecond)

	events := handler.getEvents()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Verify properties were normalized to content
	if events[0].Type != "message.part.delta" {
		t.Errorf("event 0: expected type 'message.part.delta', got '%s'", events[0].Type)
	}
	if events[0].Content == nil {
		t.Fatal("event 0: content should not be nil after normalization")
	}
	if events[0].Content["delta"] != "Hello" {
		t.Errorf("event 0: expected delta 'Hello', got '%v'", events[0].Content["delta"])
	}
	if events[0].Content["messageID"] != "msg-1" {
		t.Errorf("event 0: expected messageID 'msg-1', got '%v'", events[0].Content["messageID"])
	}

	// Verify second event
	if events[1].Content["text"] != "Hello world" {
		t.Errorf("event 1: expected text 'Hello world', got '%v'", events[1].Content["text"])
	}

	// Verify third event
	if events[2].Content["status"] != "running" {
		t.Errorf("event 2: expected status 'running', got '%v'", events[2].Content["status"])
	}
}

func TestSSEClient_ConnectDisconnect(t *testing.T) {
	handler := newMockEventHandler()
	provider := &mockPodIPProvider{ip: "127.0.0.1"}

	client := NewSSEClient(provider, handler, SSEClientConfig{
		PodPort: 9999, // non-existent port
	})

	minionID := uuid.New()

	// Connect should start a goroutine that will fail to connect
	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx, minionID, "test-pod")

	// Disconnect should cancel the context
	client.Disconnect(minionID)
	cancel()

	// Verify connection was tracked and removed
	client.mu.Lock()
	_, exists := client.connections[minionID]
	client.mu.Unlock()

	if exists {
		t.Error("connection should have been removed after Disconnect")
	}
}

func TestSSEClient_ReplacesExistingConnection(t *testing.T) {
	handler := newMockEventHandler()
	provider := &mockPodIPProvider{ip: "127.0.0.1"}

	client := NewSSEClient(provider, handler, SSEClientConfig{
		PodPort: 9999,
	})

	minionID := uuid.New()
	ctx := context.Background()

	// Connect twice with same minion ID
	client.Connect(ctx, minionID, "pod-1")
	client.Connect(ctx, minionID, "pod-2") // Should replace

	// Should only have one connection
	client.mu.Lock()
	count := len(client.connections)
	client.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 connection, got %d", count)
	}

	client.Disconnect(minionID)
}

func TestExtractTokenUsage_MessageUpdated(t *testing.T) {
	// Load sample event from testdata
	data, err := os.ReadFile("testdata/message_updated.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}

	var event PodEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("failed to unmarshal testdata: %v", err)
	}

	usage, ok := extractTokenUsage(&event)
	if !ok {
		t.Fatal("extractTokenUsage() should return true for message.updated event")
	}

	// Validate cost extraction
	if usage.CostUSD != 0.02954525 {
		t.Errorf("expected cost=0.02954525, got %f", usage.CostUSD)
	}

	// Validate token fields
	if usage.InputTokens != 1763 {
		t.Errorf("expected input_tokens=1763, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 929 {
		t.Errorf("expected output_tokens=929, got %d", usage.OutputTokens)
	}
	if usage.ReasoningTokens != 793 {
		t.Errorf("expected reasoning_tokens=793, got %d", usage.ReasoningTokens)
	}
	if usage.CacheReadTokens != 13440 {
		t.Errorf("expected cache_read_tokens=13440, got %d", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 0 {
		t.Errorf("expected cache_write_tokens=0, got %d", usage.CacheWriteTokens)
	}
}
