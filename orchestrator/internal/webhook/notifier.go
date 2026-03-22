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

// Notification holds data for a webhook callback to the Discord bot.
type Notification struct {
	MinionID         uuid.UUID
	Type             NotificationType
	DiscordChannelID string
	PRURL            string // optional, for completed notifications
	Error            string // optional, for failed notifications
}

// Notifier sends notifications to the Discord bot webhook.
type Notifier interface {
	// Notify sends a notification to the Discord bot.
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
		"channel_id", notification.DiscordChannelID,
	)
	return nil
}

// webhookRequest is the JSON payload sent to the Discord bot webhook.
type webhookRequest struct {
	MinionID         string `json:"minion_id"`
	Type             string `json:"type"`
	DiscordChannelID string `json:"discord_channel_id"`
	PRURL            string `json:"pr_url,omitempty"`
	Error            string `json:"error,omitempty"`
}

// HTTPNotifier sends notifications to the Discord bot via HTTP.
type HTTPNotifier struct {
	logger     *slog.Logger
	webhookURL string
	apiToken   string
	client     *http.Client
}

// NewHTTPNotifier creates an HTTP notifier.
// webhookURL is the full URL to the Discord bot's webhook endpoint (e.g., http://bot:8081/webhook).
// apiToken is the INTERNAL_API_TOKEN used for authentication.
func NewHTTPNotifier(logger *slog.Logger, webhookURL, apiToken string) *HTTPNotifier {
	return &HTTPNotifier{
		logger:     logger,
		webhookURL: webhookURL,
		apiToken:   apiToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Notify sends a notification to the Discord bot webhook.
func (n *HTTPNotifier) Notify(ctx context.Context, notification Notification) error {
	payload := webhookRequest{
		MinionID:         notification.MinionID.String(),
		Type:             string(notification.Type),
		DiscordChannelID: notification.DiscordChannelID,
		PRURL:            notification.PRURL,
		Error:            notification.Error,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
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
		"channel_id", notification.DiscordChannelID,
	)
	return nil
}
