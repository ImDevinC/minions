// Package orchestrator provides a client for the orchestrator API.
package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Config holds orchestrator client configuration.
type Config struct {
	BaseURL  string
	APIToken string
}

// Client is an HTTP client for the orchestrator API.
type Client struct {
	baseURL  string
	apiToken string
	client   *http.Client
	logger   *slog.Logger
}

// NewClient creates a new orchestrator client.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{
		baseURL:  cfg.BaseURL,
		apiToken: cfg.APIToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// CreateMinionRequest is the request body for POST /api/minions.
type CreateMinionRequest struct {
	Repo            string `json:"repo"`
	Task            string `json:"task"`
	Model           string `json:"model,omitempty"`
	Platform        string `json:"platform"`
	Branch          string `json:"branch,omitempty"`
	SourcePRURL     string `json:"source_pr_url,omitempty"`
	GitHubCommentID string `json:"github_comment_id,omitempty"`
	GitHubUserID    string `json:"github_user_id"`
	GitHubUsername  string `json:"github_username"`
}

// CreateMinionResponse is the response body for POST /api/minions.
type CreateMinionResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// ActiveMinionResponse is the response for GET /api/minions/active-for-pr.
type ActiveMinionResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// CreateMinion creates a new minion via the orchestrator API.
func (c *Client) CreateMinion(ctx context.Context, req CreateMinionRequest) (*CreateMinionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/minions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("orchestrator returned %d: %s", resp.StatusCode, errResp.Error)
	}

	var result CreateMinionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	c.logger.Info("created minion",
		"minion_id", result.ID,
		"status", result.Status,
		"duplicate", result.Duplicate,
	)

	return &result, nil
}

// GetActiveForPR checks if there's an active minion for the given PR URL.
// Returns nil if no active minion exists.
func (c *Client) GetActiveForPR(ctx context.Context, prURL string) (*ActiveMinionResponse, error) {
	reqURL := c.baseURL + "/api/minions/active-for-pr?pr_url=" + url.QueryEscape(prURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 404 means no active minion - not an error
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("orchestrator returned %d: %s", resp.StatusCode, errResp.Error)
	}

	var result ActiveMinionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}
