package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// mockDiscordSession creates a mock Discord session for testing.
// We can't fully mock discordgo.Session since it has many internal fields,
// so we use httptest to mock the Discord API.
func setupMockDiscord(t *testing.T) (*discordgo.Session, *httptest.Server) {
	t.Helper()

	// Create a mock Discord API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a mock message response for channel message sends
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"mock-message-id","channel_id":"mock-channel-id","content":"test"}`))
	}))

	// Create a real Discord session but override the endpoint
	discord, err := discordgo.New("Bot fake-token")
	if err != nil {
		t.Fatal(err)
	}
	discord.Client = mockServer.Client()

	// Override the Discord API URL to use our mock server
	// Note: discordgo hardcodes the URL, so we need to use a custom client
	// that intercepts requests. For simplicity, we'll test the handler logic
	// without actually sending to Discord.

	return discord, mockServer
}

func TestWebhookHandler_ValidateAuth(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	tests := []struct {
		name       string
		authHeader string
		want       bool
	}{
		{"valid token", "Bearer test-token-123", true},
		{"invalid token", "Bearer wrong-token", false},
		{"missing bearer prefix", "test-token-123", false},
		{"empty header", "", false},
		{"bearer only", "Bearer ", false},
		{"wrong prefix", "Basic test-token-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			got := h.validateAuth(req)
			if got != tt.want {
				t.Errorf("validateAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookHandler_Handle_Unauthorized(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWebhookHandler_Handle_InvalidJSON(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(`invalid json`))
	req.Header.Set("Authorization", "Bearer test-token-123")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookHandler_Handle_InvalidMinionID(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	body := WebhookRequest{
		MinionID:         "not-a-uuid",
		Type:             NotifyCompleted,
		DiscordChannelID: "123456789",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer test-token-123")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookHandler_Handle_MissingChannelID(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	body := WebhookRequest{
		MinionID:         "550e8400-e29b-41d4-a716-446655440000",
		Type:             NotifyCompleted,
		DiscordChannelID: "",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer test-token-123")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookHandler_Handle_UnknownType(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	discord, mockServer := setupMockDiscord(t)
	defer mockServer.Close()

	h := NewWebhookHandler(logger, discord, "test-token-123")

	body := WebhookRequest{
		MinionID:         "550e8400-e29b-41d4-a716-446655440000",
		Type:             "unknown_type",
		DiscordChannelID: "123456789",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer test-token-123")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestWebhookHandler_Handle_AllTypes tests that all notification types
// are handled without panicking. Since we can't easily mock discordgo's
// ChannelMessageSend, we focus on validating the handler logic.
func TestWebhookHandler_Handle_AllTypes(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create mock Discord API server that accepts all message sends
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg-id","channel_id":"ch-id","content":"test"}`))
	}))
	defer mockAPI.Close()

	// Create a session that points to our mock (discordgo doesn't make this easy)
	discord, err := discordgo.New("Bot fake-token")
	if err != nil {
		t.Fatal(err)
	}
	// Note: discordgo hardcodes the Discord API URL internally
	// For now, we'll test that the handler doesn't panic and returns 200
	// The actual Discord API call will fail, but the handler catches the error

	h := NewWebhookHandler(logger, discord, "test-token-123")

	types := []struct {
		notifType NotificationType
		prURL     string
		errMsg    string
	}{
		{NotifyCompleted, "https://github.com/org/repo/pull/1", ""},
		{NotifyCompleted, "", ""}, // no PR (no changes)
		{NotifyFailed, "", "Some error message"},
		{NotifyFailed, "", ""}, // no error message
		{NotifyTerminated, "", ""},
		{NotifyIdle, "", ""},
		{NotifyClarificationTimeout, "", ""},
	}

	for _, tt := range types {
		t.Run(string(tt.notifType), func(t *testing.T) {
			body := WebhookRequest{
				MinionID:         "550e8400-e29b-41d4-a716-446655440000",
				Type:             tt.notifType,
				DiscordChannelID: "123456789",
				PRURL:            tt.prURL,
				Error:            tt.errMsg,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(bodyBytes))
			req.Header.Set("Authorization", "Bearer test-token-123")
			rec := httptest.NewRecorder()

			h.Handle(rec, req)

			// Handler should return 200 even if Discord send fails
			// (we don't want orchestrator to retry)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"550e8400-e29b-41d4-a716-446655440000", "550e8400"},
		{"abcdefgh-ijkl-mnop-qrst-uvwxyz123456", "abcdefgh"},
		{"short", "short"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := shortID(tt.id)
			if got != tt.want {
				t.Errorf("shortID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
