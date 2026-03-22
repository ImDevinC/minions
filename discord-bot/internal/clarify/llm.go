// Package clarify provides clarification LLM integration for task evaluation.
package clarify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMResponse represents the structured response from the clarification LLM.
type LLMResponse struct {
	// Ready indicates if the task is clear enough to proceed without clarification.
	Ready bool `json:"ready"`
	// Question is the clarification question to ask the user (empty if Ready is true).
	Question string `json:"question,omitempty"`
}

// LLM is the interface for clarification LLM calls.
type LLM interface {
	// Evaluate sends the task to the LLM and returns whether it's ready or needs clarification.
	Evaluate(ctx context.Context, repo, task string) (*LLMResponse, error)
}

// AnthropicClient calls the Anthropic API for clarification evaluation.
type AnthropicClient struct {
	apiKey     string
	httpClient *http.Client
	model      string
}

// NewAnthropicClient creates a new Anthropic clarification client.
func NewAnthropicClient(apiKey string) *AnthropicClient {
	return &AnthropicClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: "claude-sonnet-4-5-20250514", // Latest sonnet for quick clarification
	}
}

// clarificationPrompt is the system prompt for the clarification LLM.
const clarificationPrompt = `You are a task clarification assistant for an AI coding agent. Your job is to evaluate if a task description is clear enough for the agent to execute.

Given a repository and task description, determine:
1. Is the task clear and specific enough to implement?
2. Does the task have a single, well-defined goal?
3. Can the agent reasonably infer what files/code to modify?

If the task is clear and actionable, respond with exactly:
READY

If the task needs clarification, respond with a single, focused question that would help make the task clearer. Keep questions concise and specific.

Examples of tasks that are READY:
- "Add a --dry-run flag to the deploy command that shows what would be deployed without actually deploying"
- "Fix the bug where login fails when password contains special characters"
- "Update the README to document the new --verbose option"

Examples of tasks that need clarification:
- "Make it better" → "What specific aspect would you like improved (performance, readability, error handling)?"
- "Add logging" → "Where should logging be added and what level (debug, info, error)?"
- "Fix the tests" → "Which test file or test case is failing?"

Only ask ONE question. Be direct and specific.`

// anthropicRequest is the request body for the Anthropic API.
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response body from the Anthropic API.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Evaluate sends the task to Claude and returns whether it's ready or needs clarification.
func (c *AnthropicClient) Evaluate(ctx context.Context, repo, task string) (*LLMResponse, error) {
	userContent := fmt.Sprintf("Repository: %s\n\nTask: %s", repo, task)

	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 256, // Clarification responses should be short
		System:    clarificationPrompt,
		Messages: []message{
			{Role: "user", Content: userContent},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiResp anthropicResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Error != nil {
			return nil, fmt.Errorf("anthropic error: %s - %s", apiResp.Error.Type, apiResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from anthropic")
	}

	// Extract text response
	text := ""
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	text = strings.TrimSpace(text)

	// Check if the response indicates ready
	if strings.ToUpper(text) == "READY" {
		return &LLMResponse{Ready: true}, nil
	}

	// Otherwise, the text is a clarification question
	return &LLMResponse{Ready: false, Question: text}, nil
}

// NoOpLLM is a no-op implementation for testing or when clarification is disabled.
type NoOpLLM struct {
	// AlwaysReady makes all tasks proceed without clarification.
	AlwaysReady bool
}

// Evaluate implements LLM interface.
func (n *NoOpLLM) Evaluate(_ context.Context, _, _ string) (*LLMResponse, error) {
	if n.AlwaysReady {
		return &LLMResponse{Ready: true}, nil
	}
	return &LLMResponse{Ready: false, Question: "What specific changes do you want?"}, nil
}
