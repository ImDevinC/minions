// Package webhook provides notification functionality for the orchestrator.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// NotificationType represents the type of notification being sent.
type NotificationType string

const (
	NotifyTerminated           NotificationType = "terminated"
	NotifyCompleted            NotificationType = "completed"
	NotifyFailed               NotificationType = "failed"
	NotifyIdle                 NotificationType = "idle"
	NotifyClarificationTimeout NotificationType = "clarification_timeout"
)

// Platform represents the chat platform for notifications.
type Platform string

const (
	PlatformDiscord Platform = "discord"
	PlatformMatrix  Platform = "matrix"
)

// Notification holds data for a webhook callback to bot services.
type Notification struct {
	MinionID         uuid.UUID
	Type             NotificationType
	Platform         Platform
	DiscordChannelID string // for Discord notifications
	MatrixRoomID     string // for Matrix notifications
	PRURL            string // optional, for completed notifications
	Error            string // optional, for failed notifications
	Summary          string // optional, final AI summary message
}

// Notifier sends notifications to bot webhooks (Discord, Matrix).
type Notifier interface {
	// Notify sends a notification to the appropriate bot based on platform.
	// Returns nil if notification was sent successfully or if no webhook is configured.
	Notify(ctx context.Context, notification Notification) error
}

// NoOpNotifier is a stub implementation that does nothing.
// Use this when webhooks are not configured or for testing.
type NoOpNotifier struct {
	logger *slog.Logger
}

// NewNoOpNotifier creates a no-op notifier.
func NewNoOpNotifier(logger *slog.Logger) *NoOpNotifier {
	return &NoOpNotifier{logger: logger}
}

// Notify logs the notification but does nothing.
func (n *NoOpNotifier) Notify(ctx context.Context, notification Notification) error {
	n.logger.Info("no-op notification (webhook not configured)",
		"minion_id", notification.MinionID,
		"type", notification.Type,
		"platform", notification.Platform,
	)
	return nil
}

// webhookRequest is the JSON payload sent to bot webhooks.
type webhookRequest struct {
	MinionID         string `json:"minion_id"`
	Type             string `json:"type"`
	Platform         string `json:"platform"`
	DiscordChannelID string `json:"discord_channel_id,omitempty"`
	MatrixRoomID     string `json:"matrix_room_id,omitempty"`
	PRURL            string `json:"pr_url,omitempty"`
	Error            string `json:"error,omitempty"`
	Summary          string `json:"summary,omitempty"`
}

// HTTPNotifier sends notifications to bot services via HTTP.
type HTTPNotifier struct {
	logger     *slog.Logger
	discordURL string // Discord bot webhook URL
	matrixURL  string // Matrix bot webhook URL (optional)
	apiToken   string
	client     *http.Client
}

// HTTPNotifierConfig holds configuration for HTTPNotifier.
type HTTPNotifierConfig struct {
	Logger     *slog.Logger
	DiscordURL string // Discord bot webhook URL (e.g., http://discord-bot:8081/webhook)
	MatrixURL  string // Matrix bot webhook URL (e.g., http://matrix-bot:8081/webhook)
	APIToken   string // INTERNAL_API_TOKEN for authentication
}

// NewHTTPNotifier creates an HTTP notifier.
// webhookURL is the full URL to the Discord bot's webhook endpoint (e.g., http://bot:8081/webhook).
// apiToken is the INTERNAL_API_TOKEN used for authentication.
func NewHTTPNotifier(logger *slog.Logger, webhookURL, apiToken string) *HTTPNotifier {
	return &HTTPNotifier{
		logger:     logger,
		discordURL: webhookURL,
		apiToken:   apiToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewHTTPNotifierWithConfig creates an HTTP notifier with full configuration.
func NewHTTPNotifierWithConfig(cfg HTTPNotifierConfig) *HTTPNotifier {
	return &HTTPNotifier{
		logger:     cfg.Logger,
		discordURL: cfg.DiscordURL,
		matrixURL:  cfg.MatrixURL,
		apiToken:   cfg.APIToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Notify sends a notification to the appropriate bot based on platform.
func (n *HTTPNotifier) Notify(ctx context.Context, notification Notification) error {
	// Determine webhook URL based on platform
	var webhookURL string
	switch notification.Platform {
	case PlatformMatrix:
		if n.matrixURL == "" {
			n.logger.Warn("matrix webhook not configured, skipping notification",
				"minion_id", notification.MinionID,
				"type", notification.Type,
			)
			return nil
		}
		webhookURL = n.matrixURL
	default:
		// Default to Discord for backward compatibility
		webhookURL = n.discordURL
	}

	payload := webhookRequest{
		MinionID:         notification.MinionID.String(),
		Type:             string(notification.Type),
		Platform:         string(notification.Platform),
		DiscordChannelID: notification.DiscordChannelID,
		MatrixRoomID:     notification.MatrixRoomID,
		PRURL:            notification.PRURL,
		Error:            notification.Error,
		Summary:          notification.Summary,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.apiToken)

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	n.logger.Info("webhook notification sent",
		"minion_id", notification.MinionID,
		"type", notification.Type,
		"platform", notification.Platform,
	)
	return nil
}
