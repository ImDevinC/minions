// Package handler provides handlers for the Discord bot.
package handler

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

// NotificationType represents the type of notification from the orchestrator.
type NotificationType string

const (
	NotifyTerminated           NotificationType = "terminated"
	NotifyCompleted            NotificationType = "completed"
	NotifyFailed               NotificationType = "failed"
	NotifyIdle                 NotificationType = "idle"
	NotifyClarificationTimeout NotificationType = "clarification_timeout"
)

// WebhookRequest is the request body from the orchestrator.
type WebhookRequest struct {
	MinionID         string           `json:"minion_id"`
	Type             NotificationType `json:"type"`
	DiscordChannelID string           `json:"discord_channel_id"`
	PRURL            string           `json:"pr_url,omitempty"`
	Error            string           `json:"error,omitempty"`
	Summary          string           `json:"summary,omitempty"`
}

// WebhookHandler handles incoming webhook callbacks from the orchestrator.
type WebhookHandler struct {
	logger   *slog.Logger
	discord  *discordgo.Session
	apiToken string
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(logger *slog.Logger, discord *discordgo.Session, apiToken string) *WebhookHandler {
	return &WebhookHandler{
		logger:   logger,
		discord:  discord,
		apiToken: apiToken,
	}
}

// Handle processes incoming webhook requests from the orchestrator.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Validate Authorization header
	if !h.validateAuth(r) {
		h.logger.Warn("webhook request with invalid auth",
			"remote_addr", r.RemoteAddr,
		)
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode webhook request", "error", err)
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate minion ID
	if _, err := uuid.Parse(req.MinionID); err != nil {
		h.logger.Error("invalid minion ID in webhook", "minion_id", req.MinionID)
		http.Error(w, `{"error":"invalid minion_id"}`, http.StatusBadRequest)
		return
	}

	// Validate channel ID
	if req.DiscordChannelID == "" {
		h.logger.Error("missing discord_channel_id in webhook", "minion_id", req.MinionID)
		http.Error(w, `{"error":"missing discord_channel_id"}`, http.StatusBadRequest)
		return
	}

	h.logger.Info("received webhook callback",
		"minion_id", req.MinionID,
		"type", req.Type,
		"channel_id", req.DiscordChannelID,
	)

	// Process based on notification type
	var err error
	switch req.Type {
	case NotifyCompleted:
		err = h.handleCompleted(req)
	case NotifyFailed:
		err = h.handleFailed(req)
	case NotifyTerminated:
		err = h.handleTerminated(req)
	case NotifyIdle:
		err = h.handleIdle(req)
	case NotifyClarificationTimeout:
		err = h.handleClarificationTimeout(req)
	default:
		h.logger.Warn("unknown notification type", "type", req.Type)
		http.Error(w, `{"error":"unknown notification type"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Error("failed to handle webhook",
			"error", err,
			"type", req.Type,
			"minion_id", req.MinionID,
		)
		// Return 200 anyway - we don't want the orchestrator to retry
		// since the minion state is already updated
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"success":true}`))
}

// validateAuth checks the Authorization header against the API token.
func (h *WebhookHandler) validateAuth(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}

	token := auth[len(prefix):]
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.apiToken)) == 1
}

// handleCompleted sends a PR URL notification to Discord.
func (h *WebhookHandler) handleCompleted(req WebhookRequest) error {
	var msg string
	if req.PRURL != "" {
		msg = fmt.Sprintf("✅ Minion completed! PR created: %s", req.PRURL)
	} else {
		msg = "✅ Minion completed! No changes were made."
	}

	// Append summary if available (truncated for Discord limits)
	if req.Summary != "" {
		summary := req.Summary
		// Truncate summary to fit Discord message limits (leaving room for PR URL)
		const maxSummaryLen = 1500
		if len(summary) > maxSummaryLen {
			summary = summary[:maxSummaryLen] + "..."
		}
		msg += fmt.Sprintf("\n\n**Summary:**\n%s", summary)
	}

	_, err := h.discord.ChannelMessageSend(req.DiscordChannelID, msg)
	if err != nil {
		return fmt.Errorf("failed to send completion message: %w", err)
	}

	h.logger.Info("sent completion notification",
		"minion_id", req.MinionID,
		"channel_id", req.DiscordChannelID,
		"has_pr", req.PRURL != "",
		"has_summary", req.Summary != "",
	)
	return nil
}

// handleFailed sends an error notification to Discord.
func (h *WebhookHandler) handleFailed(req WebhookRequest) error {
	errorSummary := req.Error
	if errorSummary == "" {
		errorSummary = "Unknown error"
	}

	// Truncate very long error messages for Discord
	const maxErrorLen = 500
	if len(errorSummary) > maxErrorLen {
		errorSummary = errorSummary[:maxErrorLen] + "..."
	}

	msg := fmt.Sprintf("❌ Minion failed: %s", errorSummary)

	_, err := h.discord.ChannelMessageSend(req.DiscordChannelID, msg)
	if err != nil {
		return fmt.Errorf("failed to send failure message: %w", err)
	}

	h.logger.Info("sent failure notification",
		"minion_id", req.MinionID,
		"channel_id", req.DiscordChannelID,
		"error", req.Error,
	)
	return nil
}

// handleTerminated sends a termination notification to Discord.
func (h *WebhookHandler) handleTerminated(req WebhookRequest) error {
	// Note: we don't have the user info who terminated it in the webhook payload
	// If needed, we could extend the orchestrator's Notification struct
	msg := fmt.Sprintf("🛑 Minion `%s` was terminated.", shortID(req.MinionID))

	_, err := h.discord.ChannelMessageSend(req.DiscordChannelID, msg)
	if err != nil {
		return fmt.Errorf("failed to send termination message: %w", err)
	}

	h.logger.Info("sent termination notification",
		"minion_id", req.MinionID,
		"channel_id", req.DiscordChannelID,
	)
	return nil
}

// handleIdle sends an idle warning to Discord.
func (h *WebhookHandler) handleIdle(req WebhookRequest) error {
	msg := fmt.Sprintf("⚠️ Minion `%s` has been idle for over 30 minutes. It may be stuck or waiting for input.", shortID(req.MinionID))

	_, err := h.discord.ChannelMessageSend(req.DiscordChannelID, msg)
	if err != nil {
		return fmt.Errorf("failed to send idle message: %w", err)
	}

	h.logger.Info("sent idle notification",
		"minion_id", req.MinionID,
		"channel_id", req.DiscordChannelID,
	)
	return nil
}

// handleClarificationTimeout sends a timeout notification to Discord.
func (h *WebhookHandler) handleClarificationTimeout(req WebhookRequest) error {
	msg := fmt.Sprintf("⏰ Minion `%s` timed out waiting for clarification (24h limit). The task has been cancelled.", shortID(req.MinionID))

	_, err := h.discord.ChannelMessageSend(req.DiscordChannelID, msg)
	if err != nil {
		return fmt.Errorf("failed to send clarification timeout message: %w", err)
	}

	h.logger.Info("sent clarification timeout notification",
		"minion_id", req.MinionID,
		"channel_id", req.DiscordChannelID,
	)
	return nil
}

// shortID returns the first 8 characters of a UUID for display.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
