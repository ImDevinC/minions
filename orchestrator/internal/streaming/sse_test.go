package streaming

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
		{
			name: "zero tokens not reported",
			event: PodEvent{
				Type: "token_usage",
				Content: map[string]any{
					"input_tokens":  float64(0),
					"output_tokens": float64(0),
				},
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

func TestSSEClient_ConnectDisconnect(t *testing.T) {
	handler := newMockEventHandler()
	provider := &mockPodIPProvider{ip: "127.0.0.1"}

	client := NewSSEClient(provider, handler, SSEClientConfig{
		PodPort: 9999, // non-existent port
	})

	minionID := uuid.New()

	// Connect should start a goroutine that will fail to connect
	ctx, cancel := context.WithCancel(context.Background())
	client.Connect(ctx, minionID, "test-pod", "")

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
	client.Connect(ctx, minionID, "pod-1", "")
	client.Connect(ctx, minionID, "pod-2", "") // Should replace

	// Should only have one connection
	client.mu.Lock()
	count := len(client.connections)
	client.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 connection, got %d", count)
	}

	client.Disconnect(minionID)
}

func TestSSEClient_HTTPBasicAuthHeader(t *testing.T) {
	tests := []struct {
		name            string
		password        string
		wantAuthValue   string
		wantAuthPresent bool
	}{
		{
			name:            "simple password",
			password:        "test-password-123",
			wantAuthValue:   "Basic b3BlbmNvZGU6dGVzdC1wYXNzd29yZC0xMjM=",
			wantAuthPresent: true,
		},
		{
			name:            "password with special characters",
			password:        "p@ss:w0rd!#$%",
			wantAuthValue:   "Basic b3BlbmNvZGU6cEBzczp3MHJkISMkJQ==",
			wantAuthPresent: true,
		},
		{
			name:            "uuid password",
			password:        "550e8400-e29b-41d4-a716-446655440000",
			wantAuthValue:   "Basic b3BlbmNvZGU6NTUwZTg0MDAtZTI5Yi00MWQ0LWE3MTYtNDQ2NjU1NDQwMDAw",
			wantAuthPresent: true,
		},
		{
			name:            "empty password",
			password:        "",
			wantAuthValue:   "Basic b3BlbmNvZGU6",
			wantAuthPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuthHeader string
			requestReceived := make(chan struct{})

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture the Authorization header
				capturedAuthHeader = r.Header.Get("Authorization")

				// Verify endpoint path is /event (singular, not /events)
				if r.URL.Path != "/event" {
					t.Errorf("expected path /event, got %s", r.URL.Path)
				}

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				close(requestReceived)
				// Close immediately after headers
			}))
			defer server.Close()

			handler := newMockEventHandler()
			// Extract host from server URL (without port - we'll use a custom port via PodIPProvider)
			// The server.URL is like "http://127.0.0.1:12345", we need just the IP
			serverHost := server.Listener.Addr().(*net.TCPAddr).IP.String()
			serverPort := server.Listener.Addr().(*net.TCPAddr).Port

			provider := &mockPodIPProvider{ip: serverHost}

			client := NewSSEClient(provider, handler, SSEClientConfig{
				PodPort: serverPort, // Use the actual test server port
			})

			minionID := uuid.New()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Call streamEvents directly to test auth header
			err := client.streamEvents(ctx, minionID, "test-pod", tt.password)
			if err != nil {
				// Expected to fail since we close connection immediately
				t.Logf("streamEvents returned: %v", err)
			}

			// Wait for request to be received
			select {
			case <-requestReceived:
			case <-time.After(1 * time.Second):
				t.Fatal("request not received within timeout")
			}

			// Verify Authorization header was set correctly
			if tt.wantAuthPresent && capturedAuthHeader != tt.wantAuthValue {
				t.Errorf("Authorization header = %q, want %q", capturedAuthHeader, tt.wantAuthValue)
			}

			if !tt.wantAuthPresent && capturedAuthHeader != "" {
				t.Errorf("Authorization header should be empty, got %q", capturedAuthHeader)
			}
		})
	}
}
