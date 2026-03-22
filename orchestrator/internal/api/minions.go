// Package api provides HTTP handlers and routing for the orchestrator.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/anomalyco/minions/orchestrator/internal/db"
)

// MinionHandler handles minion-related HTTP endpoints.
type MinionHandler struct {
	users   *db.UserStore
	minions *db.MinionStore
	logger  *slog.Logger
}

// NewMinionHandler creates a new MinionHandler.
func NewMinionHandler(users *db.UserStore, minions *db.MinionStore, logger *slog.Logger) *MinionHandler {
	return &MinionHandler{
		users:   users,
		minions: minions,
		logger:  logger,
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
