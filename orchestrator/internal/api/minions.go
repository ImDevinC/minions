// Package api provides HTTP handlers and routing for the orchestrator.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SSEDisconnector handles disconnecting SSE streams from minions.
type SSEDisconnector interface {
	// Disconnect stops streaming events for a minion.
	Disconnect(minionID uuid.UUID)
}

// MinionHandler handles minion-related HTTP endpoints.
type MinionHandler struct {
	users         *db.UserStore
	minions       *db.MinionStore
	events        *db.EventStore
	podTerminator k8s.PodTerminator
	notifier      webhook.Notifier
	sse           SSEDisconnector
	logger        *slog.Logger
}

// MinionHandlerConfig holds dependencies for MinionHandler.
type MinionHandlerConfig struct {
	Users         *db.UserStore
	Minions       *db.MinionStore
	Events        *db.EventStore
	PodTerminator k8s.PodTerminator
	Notifier      webhook.Notifier
	SSE           SSEDisconnector
	Logger        *slog.Logger
}

// NewMinionHandler creates a new MinionHandler.
func NewMinionHandler(cfg MinionHandlerConfig) *MinionHandler {
	return &MinionHandler{
		users:         cfg.Users,
		minions:       cfg.Minions,
		events:        cfg.Events,
		podTerminator: cfg.PodTerminator,
		notifier:      cfg.Notifier,
		sse:           cfg.SSE,
		logger:        cfg.Logger,
	}
}

// CreateMinionRequest is the request body for POST /api/minions.
type CreateMinionRequest struct {
	Repo             string `json:"repo"`
	Task             string `json:"task"`
	Model            string `json:"model"`
	DiscordMessageID string `json:"discord_message_id"`
	DiscordChannelID string `json:"discord_channel_id"`
	DiscordUserID    string `json:"discord_user_id"`
	DiscordUsername  string `json:"discord_username"`
}

// CreateMinionResponse is the response body for POST /api/minions.
type CreateMinionResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Repo      string `json:"repo"`
	Task      string `json:"task"`
	CreatedAt string `json:"created_at"`
	Duplicate bool   `json:"duplicate,omitempty"` // true if returning existing minion
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// repoRegex validates repo format: owner/repo with optional subgroups for nested repos
var repoRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(/[a-zA-Z0-9_.-]+)*$`)

// HandleCreate handles POST /api/minions.
func (h *MinionHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateMinionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	// Validate required fields
	if req.Repo == "" || req.Task == "" || req.Model == "" || req.DiscordUserID == "" {
		h.writeError(w, http.StatusBadRequest, "missing required fields: repo, task, model, discord_user_id", "")
		return
	}

	// Validate repo format
	if !repoRegex.MatchString(req.Repo) {
		h.writeError(w, http.StatusBadRequest, "invalid repo format: expected owner/repo", "INVALID_REPO")
		return
	}

	// Normalize repo (trim whitespace)
	req.Repo = strings.TrimSpace(req.Repo)

	// Get or create user
	user, created, err := h.users.GetOrCreate(r.Context(), req.DiscordUserID, req.DiscordUsername)
	if err != nil {
		h.logger.Error("failed to get or create user", "error", err, "discord_id", req.DiscordUserID)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}
	if created {
		h.logger.Info("created new user", "user_id", user.ID, "discord_id", req.DiscordUserID)
	}

	// Check rate limits
	rateInfo, err := h.minions.GetRateLimitInfo(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("failed to get rate limit info", "error", err, "user_id", user.ID)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	if rateInfo.HourlyCount >= db.MaxMinionsPerHour {
		h.logger.Warn("rate limit exceeded", "user_id", user.ID, "hourly_count", rateInfo.HourlyCount)
		h.writeError(w, http.StatusTooManyRequests, "rate limit exceeded: maximum 10 minions per hour", "RATE_LIMIT_EXCEEDED")
		return
	}

	if rateInfo.ConcurrentCount >= db.MaxConcurrentMinions {
		h.logger.Warn("concurrent limit exceeded", "user_id", user.ID, "concurrent_count", rateInfo.ConcurrentCount)
		h.writeError(w, http.StatusTooManyRequests, "concurrent limit exceeded: maximum 3 pending/running minions", "CONCURRENT_LIMIT_EXCEEDED")
		return
	}

	// Create minion (with duplicate detection)
	result, err := h.minions.CreateOrFindDuplicate(r.Context(), db.CreateMinionParams{
		UserID:           user.ID,
		Repo:             req.Repo,
		Task:             req.Task,
		Model:            req.Model,
		DiscordMessageID: req.DiscordMessageID,
		DiscordChannelID: req.DiscordChannelID,
	})
	if err != nil {
		h.logger.Error("failed to create minion", "error", err, "user_id", user.ID)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	minion := result.Minion

	if result.WasDuplicate {
		h.logger.Info("duplicate minion detected",
			"minion_id", minion.ID,
			"user_id", user.ID,
			"repo", req.Repo,
		)
	} else {
		h.logger.Info("created minion",
			"minion_id", minion.ID,
			"user_id", user.ID,
			"repo", req.Repo,
			"model", req.Model,
		)
	}

	resp := CreateMinionResponse{
		ID:        minion.ID.String(),
		Status:    string(minion.Status),
		Repo:      minion.Repo,
		Task:      minion.Task,
		CreatedAt: minion.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Duplicate: result.WasDuplicate,
	}

	w.Header().Set("Content-Type", "application/json")
	// Return 200 for duplicate (existing resource), 201 for newly created
	if result.WasDuplicate {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

func (h *MinionHandler) writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse{Error: message}
	if code != "" {
		resp.Code = code
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// RateLimitError wraps rate limit errors with detail.
type RateLimitError struct {
	Type    string // "hourly" or "concurrent"
	Current int
	Max     int
}

func (e *RateLimitError) Error() string {
	if e.Type == "hourly" {
		return "rate limit exceeded"
	}
	return "concurrent limit exceeded"
}

// IsRateLimitError checks if an error is a rate limit error.
func IsRateLimitError(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}

// ListMinionResponse is a single minion in the list response.
type ListMinionResponse struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Repo      string  `json:"repo"`
	Task      string  `json:"task"`
	Model     string  `json:"model"`
	PRURL     *string `json:"pr_url,omitempty"`
	Error     *string `json:"error,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
	CreatedAt string  `json:"created_at"`
}

// HandleList handles GET /api/minions.
// Query params:
//   - status: filter by status (pending, running, completed, failed, etc.)
//   - limit: max results (default 50, max 200)
func (h *MinionHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	params := db.ListMinionsParams{}

	// Parse status filter
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := db.MinionStatus(statusStr)
		// Validate status is a known value
		switch status {
		case db.StatusPending, db.StatusAwaitingClarification, db.StatusRunning,
			db.StatusCompleted, db.StatusFailed, db.StatusTerminated:
			params.Status = &status
		default:
			h.writeError(w, http.StatusBadRequest, "invalid status value", "INVALID_STATUS")
			return
		}
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			h.writeError(w, http.StatusBadRequest, "limit must be a positive integer", "INVALID_LIMIT")
			return
		}
		params.Limit = limit
	}

	minions, err := h.minions.List(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list minions", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	resp := make([]ListMinionResponse, len(minions))
	for i, m := range minions {
		resp[i] = ListMinionResponse{
			ID:        m.ID.String(),
			Status:    string(m.Status),
			Repo:      m.Repo,
			Task:      m.Task,
			Model:     m.Model,
			PRURL:     m.PRURL,
			Error:     m.Error,
			CostUSD:   m.CostUSD,
			CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// GetMinionResponse is the response body for GET /api/minions/:id.
type GetMinionResponse struct {
	ID                     string         `json:"id"`
	UserID                 string         `json:"user_id"`
	Repo                   string         `json:"repo"`
	Task                   string         `json:"task"`
	Model                  string         `json:"model"`
	Status                 string         `json:"status"`
	ClarificationQuestion  *string        `json:"clarification_question,omitempty"`
	ClarificationAnswer    *string        `json:"clarification_answer,omitempty"`
	ClarificationMessageID *string        `json:"clarification_message_id,omitempty"`
	InputTokens            int64          `json:"input_tokens"`
	OutputTokens           int64          `json:"output_tokens"`
	CostUSD                float64        `json:"cost_usd"`
	PRURL                  *string        `json:"pr_url,omitempty"`
	Error                  *string        `json:"error,omitempty"`
	SessionID              *string        `json:"session_id,omitempty"`
	PodName                *string        `json:"pod_name,omitempty"`
	DiscordMessageID       *string        `json:"discord_message_id,omitempty"`
	DiscordChannelID       *string        `json:"discord_channel_id,omitempty"`
	CreatedAt              string         `json:"created_at"`
	StartedAt              *string        `json:"started_at,omitempty"`
	CompletedAt            *string        `json:"completed_at,omitempty"`
	LastActivityAt         string         `json:"last_activity_at"`
	Events                 []EventSummary `json:"events"`
}

// EventSummary is a single event in the minion detail response.
type EventSummary struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	EventType string         `json:"event_type"`
	Content   map[string]any `json:"content"`
}

// HandleGet handles GET /api/minions/{id}.
func (h *MinionHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	minion, err := h.minions.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to get minion", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// Fetch recent events (last 100)
	events, err := h.events.GetRecentEvents(r.Context(), id, 100)
	if err != nil {
		h.logger.Error("failed to get events", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// Build response
	resp := GetMinionResponse{
		ID:                     minion.ID.String(),
		UserID:                 minion.UserID.String(),
		Repo:                   minion.Repo,
		Task:                   minion.Task,
		Model:                  minion.Model,
		Status:                 string(minion.Status),
		ClarificationQuestion:  minion.ClarificationQuestion,
		ClarificationAnswer:    minion.ClarificationAnswer,
		ClarificationMessageID: minion.ClarificationMessageID,
		InputTokens:            minion.InputTokens,
		OutputTokens:           minion.OutputTokens,
		CostUSD:                minion.CostUSD,
		PRURL:                  minion.PRURL,
		Error:                  minion.Error,
		SessionID:              minion.SessionID,
		PodName:                minion.PodName,
		DiscordMessageID:       minion.DiscordMessageID,
		DiscordChannelID:       minion.DiscordChannelID,
		CreatedAt:              minion.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastActivityAt:         minion.LastActivityAt.Format("2006-01-02T15:04:05Z07:00"),
		Events:                 make([]EventSummary, len(events)),
	}

	if minion.StartedAt != nil {
		ts := minion.StartedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.StartedAt = &ts
	}
	if minion.CompletedAt != nil {
		ts := minion.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &ts
	}

	for i, e := range events {
		resp.Events[i] = EventSummary{
			ID:        e.ID.String(),
			Timestamp: e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			EventType: e.EventType,
			Content:   e.Content,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// DeleteMinionResponse is the response body for DELETE /api/minions/{id}.
type DeleteMinionResponse struct {
	Success bool `json:"success"`
}

// HandleDelete handles DELETE /api/minions/{id}.
// Terminates the pod, updates status to 'terminated', and notifies Discord.
// Returns success=true even if minion was already in a terminal state (idempotent).
func (h *MinionHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	// Atomically check status and update to terminated
	result, err := h.minions.Terminate(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to terminate minion", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// If minion was actually terminated (not already terminal), clean up resources
	if result.WasTerminated {
		h.logger.Info("minion terminated",
			"minion_id", id,
			"previous_status", result.PreviousStatus,
		)

		// Clean up SSE connection
		h.sse.Disconnect(id)

		// Terminate pod if one was assigned
		if result.PodName != nil {
			if err := h.podTerminator.TerminatePod(r.Context(), *result.PodName); err != nil {
				// Log but don't fail the request; pod cleanup is best-effort
				h.logger.Error("failed to terminate pod", "error", err, "pod_name", *result.PodName)
			}
		}

		// Notify Discord bot
		if result.DiscordChannelID != nil {
			notification := webhook.Notification{
				MinionID:         id,
				Type:             webhook.NotifyTerminated,
				DiscordChannelID: *result.DiscordChannelID,
			}
			if err := h.notifier.Notify(r.Context(), notification); err != nil {
				// Log but don't fail the request; notification is best-effort
				h.logger.Error("failed to send termination notification", "error", err, "minion_id", id)
			}
		}
	} else {
		h.logger.Info("minion already in terminal state",
			"minion_id", id,
			"status", result.PreviousStatus,
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(DeleteMinionResponse{Success: true}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// CallbackRequest is the request body for POST /api/minions/{id}/callback.
type CallbackRequest struct {
	Status    string  `json:"status"`     // "completed" or "failed"
	PRURL     *string `json:"pr_url"`     // optional, for completed minions
	Error     *string `json:"error"`      // optional, for failed minions
	SessionID *string `json:"session_id"` // optional, opencode session ID
}

// CallbackResponse is the response body for POST /api/minions/{id}/callback.
type CallbackResponse struct {
	Success bool `json:"success"`
}

// HandleCallback handles POST /api/minions/{id}/callback.
// Updates minion with completion data and notifies Discord.
func (h *MinionHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	var req CallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	// Validate status
	var status db.MinionStatus
	switch req.Status {
	case "completed":
		status = db.StatusCompleted
	case "failed":
		status = db.StatusFailed
	default:
		h.writeError(w, http.StatusBadRequest, "status must be 'completed' or 'failed'", "INVALID_STATUS")
		return
	}

	// Complete the minion
	result, err := h.minions.Complete(r.Context(), db.CompleteParams{
		ID:        id,
		Status:    status,
		PRURL:     req.PRURL,
		Error:     req.Error,
		SessionID: req.SessionID,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to complete minion", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// Clean up SSE connection
	h.sse.Disconnect(id)

	// If minion was actually updated, notify Discord
	if result.WasUpdated && result.DiscordChannelID != nil {
		var notifyType webhook.NotificationType
		var prURL, errMsg string
		if status == db.StatusCompleted {
			notifyType = webhook.NotifyCompleted
			if req.PRURL != nil {
				prURL = *req.PRURL
			}
		} else {
			notifyType = webhook.NotifyFailed
			if req.Error != nil {
				errMsg = *req.Error
			}
		}

		notification := webhook.Notification{
			MinionID:         id,
			Type:             notifyType,
			DiscordChannelID: *result.DiscordChannelID,
			PRURL:            prURL,
			Error:            errMsg,
		}
		if err := h.notifier.Notify(r.Context(), notification); err != nil {
			// Log but don't fail the request; notification is best-effort
			h.logger.Error("failed to send completion notification", "error", err, "minion_id", id)
		}

		h.logger.Info("minion callback processed",
			"minion_id", id,
			"status", status,
			"previous_status", result.PreviousStatus,
		)
	} else {
		h.logger.Info("minion already in terminal state",
			"minion_id", id,
			"status", result.PreviousStatus,
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(CallbackResponse{Success: true}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// StatsResponse is the response body for GET /api/stats.
type StatsResponse struct {
	TotalCostUSD      float64              `json:"total_cost_usd"`
	TotalInputTokens  int64                `json:"total_input_tokens"`
	TotalOutputTokens int64                `json:"total_output_tokens"`
	ByModel           []ModelStatsResponse `json:"by_model"`
}

// ModelStatsResponse is per-model statistics in the stats response.
type ModelStatsResponse struct {
	Model        string  `json:"model"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Count        int64   `json:"count"`
}

// HandleStats handles GET /api/stats.
// Returns aggregate statistics across all minions.
func (h *MinionHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.minions.GetStats(r.Context())
	if err != nil {
		h.logger.Error("failed to get stats", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	resp := StatsResponse{
		TotalCostUSD:      stats.TotalCostUSD,
		TotalInputTokens:  stats.TotalInputTokens,
		TotalOutputTokens: stats.TotalOutputTokens,
		ByModel:           make([]ModelStatsResponse, len(stats.ByModel)),
	}

	for i, ms := range stats.ByModel {
		resp.ByModel[i] = ModelStatsResponse{
			Model:        ms.Model,
			CostUSD:      ms.CostUSD,
			InputTokens:  ms.InputTokens,
			OutputTokens: ms.OutputTokens,
			Count:        ms.Count,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// ClarificationRequest is the request body for PATCH /api/minions/{id}/clarification.
type ClarificationRequest struct {
	Question         string `json:"question"`
	DiscordMessageID string `json:"discord_message_id"`
}

// ClarificationAnswerRequest is the request body for PATCH /api/minions/{id}/clarification-answer.
type ClarificationAnswerRequest struct {
	Answer string `json:"answer"`
}

// ClarificationResponse is the response body for PATCH /api/minions/{id}/clarification.
type ClarificationResponse struct {
	Success bool `json:"success"`
}

// HandleClarification handles PATCH /api/minions/{id}/clarification.
// Updates minion status to awaiting_clarification with the question.
func (h *MinionHandler) HandleClarification(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	var req ClarificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	// Validate required fields
	if req.Question == "" {
		h.writeError(w, http.StatusBadRequest, "question is required", "MISSING_QUESTION")
		return
	}
	if req.DiscordMessageID == "" {
		h.writeError(w, http.StatusBadRequest, "discord_message_id is required", "MISSING_DISCORD_MESSAGE_ID")
		return
	}

	// Set clarification state
	err = h.minions.SetClarification(r.Context(), db.SetClarificationParams{
		ID:                     id,
		Question:               req.Question,
		ClarificationMessageID: req.DiscordMessageID,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, db.ErrInvalidStatusTransition) {
			h.writeError(w, http.StatusConflict, "minion is not in pending status", "INVALID_STATUS")
			return
		}
		h.logger.Error("failed to set clarification", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	h.logger.Info("clarification set",
		"minion_id", id,
		"discord_message_id", req.DiscordMessageID,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ClarificationResponse{Success: true}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// HandleClarificationAnswer handles PATCH /api/minions/{id}/clarification-answer.
// Sets the user's answer and transitions the minion back to pending status.
func (h *MinionHandler) HandleClarificationAnswer(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	var req ClarificationAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	// Validate required field
	if req.Answer == "" {
		h.writeError(w, http.StatusBadRequest, "answer is required", "MISSING_ANSWER")
		return
	}

	// Set clarification answer
	err = h.minions.SetClarificationAnswer(r.Context(), db.SetClarificationAnswerParams{
		ID:     id,
		Answer: req.Answer,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, db.ErrInvalidStatusTransition) {
			h.writeError(w, http.StatusConflict, "minion is not awaiting clarification", "INVALID_STATUS")
			return
		}
		h.logger.Error("failed to set clarification answer", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	h.logger.Info("clarification answer set",
		"minion_id", id,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ClarificationResponse{Success: true}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// GetEventsSinceResponse is the response body for GET /api/minions/{id}/events.
type GetEventsSinceResponse struct {
	Events []EventSummary `json:"events"`
}

// HandleGetEvents handles GET /api/minions/{id}/events.
// Query params:
//   - since: ISO8601 timestamp, returns events with timestamp > since
//   - limit: max events to return (default 1000, max 10000)
//
// Used for WebSocket reconnection to fetch missed events.
func (h *MinionHandler) HandleGetEvents(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid minion id", "INVALID_ID")
		return
	}

	// Parse 'since' timestamp
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		h.writeError(w, http.StatusBadRequest, "since parameter is required", "MISSING_SINCE")
		return
	}

	since, err := parseTimestamp(sinceStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid since timestamp (use ISO8601 format)", "INVALID_SINCE")
		return
	}

	// Parse optional limit
	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			h.writeError(w, http.StatusBadRequest, "limit must be a positive integer", "INVALID_LIMIT")
			return
		}
	}

	// Verify minion exists
	_, err = h.minions.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to get minion", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// Fetch events since the given timestamp
	events, err := h.events.GetEventsSince(r.Context(), id, since, limit)
	if err != nil {
		h.logger.Error("failed to get events", "error", err, "minion_id", id)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	resp := GetEventsSinceResponse{
		Events: make([]EventSummary, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = EventSummary{
			ID:        e.ID.String(),
			Timestamp: e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			EventType: e.EventType,
			Content:   e.Content,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// parseTimestamp parses an ISO8601 timestamp string.
// Supports both RFC3339 (with timezone) and RFC3339Nano (with nanoseconds).
func parseTimestamp(s string) (time.Time, error) {
	// Try RFC3339Nano first (includes sub-second precision)
	t, err := time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t, nil
	}
	// Fall back to RFC3339
	return time.Parse(time.RFC3339, s)
}

// HandleGetByClarificationMessageID handles GET /api/minions/by-clarification/{messageId}.
// Looks up a minion by its Discord clarification message ID.
func (h *MinionHandler) HandleGetByClarificationMessageID(w http.ResponseWriter, r *http.Request) {
	messageID := chi.URLParam(r, "messageId")
	if messageID == "" {
		h.writeError(w, http.StatusBadRequest, "messageId is required", "MISSING_MESSAGE_ID")
		return
	}

	minion, err := h.minions.GetByClarificationMessageID(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "minion not found", "NOT_FOUND")
			return
		}
		h.logger.Error("failed to get minion by clarification message ID", "error", err, "message_id", messageID)
		h.writeError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	// Return a minimal response with just the fields needed for reply handling
	resp := struct {
		ID                    string  `json:"id"`
		Repo                  string  `json:"repo"`
		Task                  string  `json:"task"`
		Model                 string  `json:"model"`
		Status                string  `json:"status"`
		ClarificationQuestion *string `json:"clarification_question,omitempty"`
		DiscordChannelID      *string `json:"discord_channel_id,omitempty"`
	}{
		ID:                    minion.ID.String(),
		Repo:                  minion.Repo,
		Task:                  minion.Task,
		Model:                 minion.Model,
		Status:                string(minion.Status),
		ClarificationQuestion: minion.ClarificationQuestion,
		DiscordChannelID:      minion.DiscordChannelID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}
