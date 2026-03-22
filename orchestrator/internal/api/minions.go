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

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// MinionHandler handles minion-related HTTP endpoints.
type MinionHandler struct {
	users         *db.UserStore
	minions       *db.MinionStore
	events        *db.EventStore
	podTerminator k8s.PodTerminator
	notifier      webhook.Notifier
	logger        *slog.Logger
}

// MinionHandlerConfig holds dependencies for MinionHandler.
type MinionHandlerConfig struct {
	Users         *db.UserStore
	Minions       *db.MinionStore
	Events        *db.EventStore
	PodTerminator k8s.PodTerminator
	Notifier      webhook.Notifier
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

	// Create minion
	minion, err := h.minions.Create(r.Context(), db.CreateMinionParams{
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

	h.logger.Info("created minion",
		"minion_id", minion.ID,
		"user_id", user.ID,
		"repo", req.Repo,
		"model", req.Model,
	)

	resp := CreateMinionResponse{
		ID:        minion.ID.String(),
		Status:    string(minion.Status),
		Repo:      minion.Repo,
		Task:      minion.Task,
		CreatedAt: minion.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
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
