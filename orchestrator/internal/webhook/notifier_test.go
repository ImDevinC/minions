package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestHTTPNotifier_Notify(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name         string
		notification Notification
		serverStatus int
		wantErr      bool
	}{
		{
			name: "completed with PR",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyCompleted,
				DiscordChannelID: "123456789",
				PRURL:            "https://github.com/org/repo/pull/1",
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "failed with error",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyFailed,
				DiscordChannelID: "123456789",
				Error:            "Something went wrong",
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "terminated",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyTerminated,
				DiscordChannelID: "123456789",
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "idle",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyIdle,
				DiscordChannelID: "123456789",
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "clarification_timeout",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyClarificationTimeout,
				DiscordChannelID: "123456789",
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "server returns 500",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyCompleted,
				DiscordChannelID: "123456789",
			},
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
		{
			name: "server returns 401",
			notification: Notification{
				MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
				Type:             NotifyCompleted,
				DiscordChannelID: "123456789",
			},
			serverStatus: http.StatusUnauthorized,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedReq webhookRequest
			var receivedAuth string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAuth = r.Header.Get("Authorization")

				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &receivedReq)

				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()

			notifier := NewHTTPNotifier(logger, server.URL, "test-token")
			err := notifier.Notify(context.Background(), tt.notification)

			if (err != nil) != tt.wantErr {
				t.Errorf("Notify() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify the request was correct
				if receivedAuth != "Bearer test-token" {
					t.Errorf("unexpected auth header: %s", receivedAuth)
				}
				if receivedReq.MinionID != tt.notification.MinionID.String() {
					t.Errorf("unexpected minion_id: %s", receivedReq.MinionID)
				}
				if receivedReq.Type != string(tt.notification.Type) {
					t.Errorf("unexpected type: %s", receivedReq.Type)
				}
				if receivedReq.DiscordChannelID != tt.notification.DiscordChannelID {
					t.Errorf("unexpected discord_channel_id: %s", receivedReq.DiscordChannelID)
				}
			}
		})
	}
}

func TestHTTPNotifier_Notify_NetworkError(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use a URL that will definitely fail
	notifier := NewHTTPNotifier(logger, "http://localhost:99999/webhook", "test-token")

	err := notifier.Notify(context.Background(), Notification{
		MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Type:             NotifyCompleted,
		DiscordChannelID: "123456789",
	})

	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestNoOpNotifier(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	notifier := NewNoOpNotifier(logger)

	err := notifier.Notify(context.Background(), Notification{
		MinionID:         uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Type:             NotifyCompleted,
		DiscordChannelID: "123456789",
	})

	if err != nil {
		t.Errorf("NoOpNotifier should not return error, got: %v", err)
	}
}
