// Package clarify provides clarification handling for minion tasks.
package clarify

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Retry configuration constants
const (
	MaxRetries     = 3
	InitialBackoff = 1 * time.Second
	MaxBackoff     = 30 * time.Second
	BackoffFactor  = 2.0
)

// ErrAllRetriesFailed is returned when all LLM retry attempts fail.
var ErrAllRetriesFailed = errors.New("all clarification LLM retries failed")

// Result represents the outcome of a clarification evaluation.
type Result struct {
	// Ready indicates the task is clear and can proceed.
	Ready bool
	// Question is the clarification question to ask the user (if not Ready).
	Question string
}

// Handler manages the clarification flow with retry logic.
type Handler struct {
	llm    LLM
	logger *slog.Logger
}

// NewHandler creates a new clarification handler.
func NewHandler(llm LLM, logger *slog.Logger) *Handler {
	return &Handler{
		llm:    llm,
		logger: logger,
	}
}

// EvaluateWithRetry sends the task to the clarification LLM with exponential backoff retries.
// Returns the LLM response or ErrAllRetriesFailed if all attempts fail.
func (h *Handler) EvaluateWithRetry(ctx context.Context, repo, task string) (*Result, error) {
	var lastErr error
	backoff := InitialBackoff

	for attempt := 1; attempt <= MaxRetries; attempt++ {
		h.logger.Info("evaluating task with clarification LLM",
			"attempt", attempt,
			"max_retries", MaxRetries,
			"repo", repo,
		)

		resp, err := h.llm.Evaluate(ctx, repo, task)
		if err == nil {
			h.logger.Info("clarification LLM response",
				"ready", resp.Ready,
				"has_question", resp.Question != "",
				"attempt", attempt,
			)
			return &Result{
				Ready:    resp.Ready,
				Question: resp.Question,
			}, nil
		}

		lastErr = err
		h.logger.Warn("clarification LLM call failed",
			"error", err,
			"attempt", attempt,
			"max_retries", MaxRetries,
			"next_backoff", backoff,
		)

		// Don't sleep after the last attempt
		if attempt < MaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			// Increase backoff for next attempt
			backoff = time.Duration(float64(backoff) * BackoffFactor)
			if backoff > MaxBackoff {
				backoff = MaxBackoff
			}
		}
	}

	h.logger.Error("all clarification LLM retries exhausted",
		"last_error", lastErr,
		"total_attempts", MaxRetries,
	)

	return nil, ErrAllRetriesFailed
}
