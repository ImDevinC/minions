// Package handler provides Matrix message handlers for the minion bot.
package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
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
	MinionID     string           `json:"minion_id"`
	Type         NotificationType `json:"type"`
	MatrixRoomID string           `json:"matrix_room_id"`
	PRURL        string           `json:"pr_url,omitempty"`
	Error        string           `json:"error,omitempty"`
	Summary      string           `json:"summary,omitempty"`
}

// WebhookHandler handles incoming webhook callbacks from the orchestrator.
type WebhookHandler struct {
	logger   *slog.Logger
	client   *mautrix.Client
	apiToken string
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(logger *slog.Logger, client *mautrix.Client, apiToken string) *WebhookHandler {
	return &WebhookHandler{
		logger:   logger,
		client:   client,
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

	// Validate room ID
	if req.MatrixRoomID == "" {
		h.logger.Error("missing matrix_room_id in webhook", "minion_id", req.MinionID)
		http.Error(w, `{"error":"missing matrix_room_id"}`, http.StatusBadRequest)
		return
	}

	h.logger.Info("received webhook callback",
		"minion_id", req.MinionID,
		"type", req.Type,
		"room_id", req.MatrixRoomID,
	)

	// Process based on notification type
	ctx := r.Context()
	var err error
	switch req.Type {
	case NotifyCompleted:
		err = h.handleCompleted(ctx, req)
	case NotifyFailed:
		err = h.handleFailed(ctx, req)
	case NotifyTerminated:
		err = h.handleTerminated(ctx, req)
	case NotifyIdle:
		err = h.handleIdle(ctx, req)
	case NotifyClarificationTimeout:
		err = h.handleClarificationTimeout(ctx, req)
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

// handleCompleted sends a PR URL notification to Matrix.
func (h *WebhookHandler) handleCompleted(ctx context.Context, req WebhookRequest) error {
	var msg string
	if req.PRURL != "" {
		msg = fmt.Sprintf("✅ Minion completed! PR created: %s", req.PRURL)
	} else {
		msg = "✅ Minion completed! No changes were made."
	}

	// Append summary if available (truncated for Matrix limits)
	if req.Summary != "" {
		summary := req.Summary
		// Truncate summary to fit Matrix message limits
		const maxSummaryLen = 4000
		if len(summary) > maxSummaryLen {
			summary = summary[:maxSummaryLen] + "..."
		}
		msg += fmt.Sprintf("\n\n**Summary:**\n%s", summary)
	}

	roomID := id.RoomID(req.MatrixRoomID)
	content := format.RenderMarkdown(msg, true, false)

	_, err := h.client.SendMessageEvent(ctx, roomID, event.EventMessage, &content)
	if err != nil {
		return fmt.Errorf("failed to send completion message: %w", err)
	}

	h.logger.Info("sent completion notification",
		"minion_id", req.MinionID,
		"room_id", req.MatrixRoomID,
		"has_pr", req.PRURL != "",
		"has_summary", req.Summary != "",
	)
	return nil
}

// handleFailed sends an error notification to Matrix.
func (h *WebhookHandler) handleFailed(ctx context.Context, req WebhookRequest) error {
	errorSummary := req.Error
	if errorSummary == "" {
		errorSummary = "Unknown error"
	}

	// Truncate very long error messages
	const maxErrorLen = 500
	if len(errorSummary) > maxErrorLen {
		errorSummary = errorSummary[:maxErrorLen] + "..."
	}

	msg := fmt.Sprintf("❌ Minion failed: %s", errorSummary)

	roomID := id.RoomID(req.MatrixRoomID)
	content := format.RenderMarkdown(msg, true, false)

	_, err := h.client.SendMessageEvent(ctx, roomID, event.EventMessage, &content)
	if err != nil {
		return fmt.Errorf("failed to send failure message: %w", err)
	}

	h.logger.Info("sent failure notification",
		"minion_id", req.MinionID,
		"room_id", req.MatrixRoomID,
		"error", req.Error,
	)
	return nil
}

// handleTerminated sends a termination notification to Matrix.
func (h *WebhookHandler) handleTerminated(ctx context.Context, req WebhookRequest) error {
	msg := fmt.Sprintf("🛑 Minion `%s` was terminated.", shortID(req.MinionID))

	roomID := id.RoomID(req.MatrixRoomID)
	content := format.RenderMarkdown(msg, true, false)

	_, err := h.client.SendMessageEvent(ctx, roomID, event.EventMessage, &content)
	if err != nil {
		return fmt.Errorf("failed to send termination message: %w", err)
	}

	h.logger.Info("sent termination notification",
		"minion_id", req.MinionID,
		"room_id", req.MatrixRoomID,
	)
	return nil
}

// handleIdle sends an idle warning to Matrix.
func (h *WebhookHandler) handleIdle(ctx context.Context, req WebhookRequest) error {
	msg := fmt.Sprintf("⚠️ Minion `%s` has been idle for over 30 minutes. It may be stuck or waiting for input.", shortID(req.MinionID))

	roomID := id.RoomID(req.MatrixRoomID)
	content := format.RenderMarkdown(msg, true, false)

	_, err := h.client.SendMessageEvent(ctx, roomID, event.EventMessage, &content)
	if err != nil {
		return fmt.Errorf("failed to send idle message: %w", err)
	}

	h.logger.Info("sent idle notification",
		"minion_id", req.MinionID,
		"room_id", req.MatrixRoomID,
	)
	return nil
}

// handleClarificationTimeout sends a timeout notification to Matrix.
func (h *WebhookHandler) handleClarificationTimeout(ctx context.Context, req WebhookRequest) error {
	msg := fmt.Sprintf("⏰ Minion `%s` timed out waiting for clarification (24h limit). The task has been cancelled.", shortID(req.MinionID))

	roomID := id.RoomID(req.MatrixRoomID)
	content := format.RenderMarkdown(msg, true, false)

	_, err := h.client.SendMessageEvent(ctx, roomID, event.EventMessage, &content)
	if err != nil {
		return fmt.Errorf("failed to send clarification timeout message: %w", err)
	}

	h.logger.Info("sent clarification timeout notification",
		"minion_id", req.MinionID,
		"room_id", req.MatrixRoomID,
	)
	return nil
}

// shortID returns the first 8 characters of a UUID for display.
func shortID(idStr string) string {
	if len(idStr) >= 8 {
		return idStr[:8]
	}
	return idStr
}
