// Package webhook provides notification functionality for the orchestrator.
package webhook

import (
	"context"
	"log/slog"

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
