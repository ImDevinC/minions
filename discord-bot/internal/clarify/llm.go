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

// OpenRouterClient calls the OpenRouter API for clarification evaluation.
// OpenRouter provides a unified API to access models from Anthropic, OpenAI, and others.
type OpenRouterClient struct {
	apiKey     string
	httpClient *http.Client
	model      string
}

// NewOpenRouterClient creates a new OpenRouter clarification client.
func NewOpenRouterClient(apiKey, model string) *OpenRouterClient {
	return &OpenRouterClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: model,
	}
}

// clarificationPrompt is the system prompt for the clarification LLM.
const clarificationPrompt = `You are a task clarification assistant for an AI coding agent. Your job is to evaluate if a task description is clear enough for the agent to execute.

Given a repository and task description, determine:
1. Is the task clear and specific enough to implement?
2. Does the task have a single, well-defined goal?
3. Can the agent reasonably infer what files/code to modify?

Important constraint:
- The coding agent can checkout and inspect the repository on its own.
- Never ask where code lives (file names, paths, folders, directories, modules, or repository structure).
- Only ask about product/behavior requirements when truly unclear.

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

Examples of questions to avoid (NOT allowed):
- "Which file contains this code?"
- "What folder should I edit?"
- "Where is this implemented in the repository?"

Only ask ONE question. Be direct and specific.`

var codebaseStructureQuestionPhrases = []string{
	"which file",
	"what file",
	"which files",
	"what files",
	"file contains",
	"where is this implemented",
	"where in the repo",
	"where in the repository",
	"where in the codebase",
	"which folder",
	"what folder",
	"which directory",
	"what directory",
	"which path",
	"what path",
	"repository structure",
	"repo structure",
	"codebase structure",
}

// openRouterRequest is the request body for the OpenRouter API (OpenAI-compatible format).
type openRouterRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openRouterResponse is the response body from the OpenRouter API (OpenAI-compatible format).
type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// Evaluate sends the task to Claude via OpenRouter and returns whether it's ready or needs clarification.
func (c *OpenRouterClient) Evaluate(ctx context.Context, repo, task string) (*LLMResponse, error) {
	userContent := fmt.Sprintf("Repository: %s\n\nTask: %s", repo, task)

	reqBody := openRouterRequest{
		Model:     c.model,
		MaxTokens: 256, // Clarification responses should be short
		Messages: []message{
			{Role: "system", Content: clarificationPrompt},
			{Role: "user", Content: userContent},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/imdevinc/minions")
	req.Header.Set("X-Title", "Minions Discord Bot")

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
		var apiResp openRouterResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Error != nil {
			return nil, fmt.Errorf("openrouter error: %s - %s", apiResp.Error.Type, apiResp.Error.Message)
		}
		return nil, fmt.Errorf("openrouter error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp openRouterResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from openrouter")
	}

	text := apiResp.Choices[0].Message.Content
	return parseClarificationResponse(text), nil
}

func parseClarificationResponse(text string) *LLMResponse {
	trimmed := strings.TrimSpace(text)

	// Check if the response indicates ready.
	if strings.ToUpper(trimmed) == "READY" {
		return &LLMResponse{Ready: true}
	}

	// Guardrail: do not ask users where code lives in the repository.
	if isCodebaseStructureQuestion(trimmed) {
		return &LLMResponse{Ready: true}
	}

	// Otherwise, the text is a clarification question.
	return &LLMResponse{Ready: false, Question: trimmed}
}

func isCodebaseStructureQuestion(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	for _, phrase := range codebaseStructureQuestionPhrases {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
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
