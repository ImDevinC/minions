// Package orchestrator provides an HTTP client for the orchestrator API.
package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrRateLimitExceeded is returned when the user exceeds the hourly minion limit.
var ErrRateLimitExceeded = errors.New("rate limit exceeded: maximum 10 minions per hour")

// ErrConcurrentLimitExceeded is returned when the user exceeds the concurrent minion limit.
var ErrConcurrentLimitExceeded = errors.New("concurrent limit exceeded: maximum 3 pending/running minions")

// CreateMinionRequest is the request body for POST /api/minions.
type CreateMinionRequest struct {
	Repo                       string `json:"repo"`
	Task                       string `json:"task"`
	Model                      string `json:"model"`
	Platform                   string `json:"platform"`
	InitialStatus              string `json:"initial_status,omitempty"`
	ClarificationQuestion      string `json:"clarification_question,omitempty"`
	MatrixClarificationEventID string `json:"matrix_clarification_event_id,omitempty"`
	MatrixEventID              string `json:"matrix_event_id"`
	MatrixRoomID               string `json:"matrix_room_id"`
	MatrixUserID               string `json:"matrix_user_id"`
}

// CreateMinionResponse is the response body for POST /api/minions.
type CreateMinionResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Repo      string `json:"repo"`
	Task      string `json:"task"`
	CreatedAt string `json:"created_at"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// ErrorResponse is the standard error response from the orchestrator.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// Client is an HTTP client for the orchestrator API.
type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new orchestrator client.
func NewClient(baseURL, apiToken string) *Client {
	return &Client{
		baseURL:  baseURL,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateMinion creates a new minion via the orchestrator API.
// Returns ErrRateLimitExceeded or ErrConcurrentLimitExceeded for 429 responses.
func (c *Client) CreateMinion(ctx context.Context, req CreateMinionRequest) (*CreateMinionResponse, error) {
	// Always set platform to matrix
	req.Platform = "matrix"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/minions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Handle rate limit responses
	if resp.StatusCode == http.StatusTooManyRequests {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("rate limit exceeded (could not parse error)")
		}
		switch errResp.Code {
		case "RATE_LIMIT_EXCEEDED":
			return nil, ErrRateLimitExceeded
		case "CONCURRENT_LIMIT_EXCEEDED":
			return nil, ErrConcurrentLimitExceeded
		default:
			// Fallback for unexpected code
			return nil, fmt.Errorf("rate limit: %s", errResp.Error)
		}
	}

	// Handle other errors
	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, string(respBody))
		}
		return nil, fmt.Errorf("orchestrator error: %s", errResp.Error)
	}

	// Parse success response (200 or 201)
	var minionResp CreateMinionResponse
	if err := json.Unmarshal(respBody, &minionResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &minionResp, nil
}

// ErrClarificationNotFound is returned when no minion is found for a clarification event ID.
var ErrClarificationNotFound = errors.New("no minion found for clarification event")

// MinionByClarificationResponse is the response from GET /api/minions/by-matrix-clarification/{eventId}.
type MinionByClarificationResponse struct {
	ID                    string  `json:"id"`
	Repo                  string  `json:"repo"`
	Task                  string  `json:"task"`
	Model                 string  `json:"model"`
	Status                string  `json:"status"`
	ClarificationQuestion *string `json:"clarification_question,omitempty"`
	MatrixRoomID          *string `json:"matrix_room_id,omitempty"`
	MatrixUserID          string  `json:"matrix_user_id"`
}

// GetByMatrixClarificationEventID looks up a minion by its Matrix clarification event ID.
// Used to find the minion when a user replies to a clarification question.
func (c *Client) GetByMatrixClarificationEventID(ctx context.Context, eventID string) (*MinionByClarificationResponse, error) {
	// URL-encode the event ID to handle special characters in Matrix event IDs
	encodedEventID := url.PathEscape(eventID)
	reqURL := fmt.Sprintf("%s/api/minions/by-matrix-clarification/%s", c.baseURL, encodedEventID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrClarificationNotFound
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, string(respBody))
		}
		return nil, fmt.Errorf("orchestrator error: %s", errResp.Error)
	}

	var minionResp MinionByClarificationResponse
	if err := json.Unmarshal(respBody, &minionResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &minionResp, nil
}

// SetClarificationAnswerRequest is the request body for PATCH /api/minions/{id}/clarification-answer.
type SetClarificationAnswerRequest struct {
	Answer string `json:"answer"`
}

// SetClarificationAnswer sets the user's answer and transitions the minion to pending.
// After this call, the orchestrator will spawn the pod with the updated task.
func (c *Client) SetClarificationAnswer(ctx context.Context, minionID string, answer string) error {
	req := SetClarificationAnswerRequest{Answer: answer}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/minions/%s/clarification-answer", c.baseURL, minionID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ErrClarificationNotFound
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return fmt.Errorf("http error %d: %s", resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("orchestrator error: %s", errResp.Error)
	}

	return nil
}
