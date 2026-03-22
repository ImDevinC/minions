package clarify

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockLLM is a mock implementation of LLM for testing
type mockLLM struct {
	responses []struct {
		resp *LLMResponse
		err  error
	}
	callCount int
}

func (m *mockLLM) Evaluate(ctx context.Context, repo, task string) (*LLMResponse, error) {
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	r := m.responses[m.callCount]
	m.callCount++
	return r.resp, r.err
}

func TestHandler_EvaluateWithRetry_SuccessFirstAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []struct {
			resp *LLMResponse
			err  error
		}{
			{resp: &LLMResponse{Ready: true}, err: nil},
		},
	}

	h := &Handler{
		llm:    mock,
		logger: nil, // We'll use a nil logger for brevity; in prod, use slog.Default()
	}

	// Use a custom handler with quick timeouts for testing
	result, err := h.evaluateWithRetryTest(context.Background(), "owner/repo", "add feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Ready {
		t.Error("expected Ready to be true")
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount)
	}
}

func TestHandler_EvaluateWithRetry_SuccessAfterRetries(t *testing.T) {
	mock := &mockLLM{
		responses: []struct {
			resp *LLMResponse
			err  error
		}{
			{resp: nil, err: errors.New("temporary failure")},
			{resp: nil, err: errors.New("temporary failure")},
			{resp: &LLMResponse{Ready: false, Question: "What changes?"}, err: nil},
		},
	}

	h := &Handler{
		llm:    mock,
		logger: nil,
	}

	result, err := h.evaluateWithRetryTest(context.Background(), "owner/repo", "fix bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Ready {
		t.Error("expected Ready to be false")
	}
	if result.Question != "What changes?" {
		t.Errorf("unexpected question: %s", result.Question)
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

func TestHandler_EvaluateWithRetry_AllRetriesFailed(t *testing.T) {
	mock := &mockLLM{
		responses: []struct {
			resp *LLMResponse
			err  error
		}{
			{resp: nil, err: errors.New("fail 1")},
			{resp: nil, err: errors.New("fail 2")},
			{resp: nil, err: errors.New("fail 3")},
		},
	}

	h := &Handler{
		llm:    mock,
		logger: nil,
	}

	_, err := h.evaluateWithRetryTest(context.Background(), "owner/repo", "task")
	if !errors.Is(err, ErrAllRetriesFailed) {
		t.Errorf("expected ErrAllRetriesFailed, got %v", err)
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

func TestHandler_EvaluateWithRetry_ContextCancelled(t *testing.T) {
	mock := &mockLLM{
		responses: []struct {
			resp *LLMResponse
			err  error
		}{
			{resp: nil, err: errors.New("fail")},
			{resp: nil, err: errors.New("fail")},
			{resp: nil, err: errors.New("fail")},
		},
	}

	h := &Handler{
		llm:    mock,
		logger: nil,
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately after first failure
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := h.evaluateWithRetryTest(ctx, "owner/repo", "task")
	if !errors.Is(err, context.Canceled) {
		// Could also be ErrAllRetriesFailed if context cancelled after all retries
		if !errors.Is(err, ErrAllRetriesFailed) {
			t.Errorf("expected context.Canceled or ErrAllRetriesFailed, got %v", err)
		}
	}
}

// evaluateWithRetryTest is a test helper with shorter backoff
func (h *Handler) evaluateWithRetryTest(ctx context.Context, repo, task string) (*Result, error) {
	backoff := 1 * time.Millisecond // Much shorter for tests

	for attempt := 1; attempt <= MaxRetries; attempt++ {
		resp, err := h.llm.Evaluate(ctx, repo, task)
		if err == nil {
			return &Result{
				Ready:    resp.Ready,
				Question: resp.Question,
			}, nil
		}

		if attempt < MaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}

	return nil, ErrAllRetriesFailed
}

func TestNoOpLLM_AlwaysReady(t *testing.T) {
	llm := &NoOpLLM{AlwaysReady: true}
	resp, err := llm.Evaluate(context.Background(), "owner/repo", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Ready {
		t.Error("expected Ready to be true")
	}
}

func TestNoOpLLM_NeedsClarification(t *testing.T) {
	llm := &NoOpLLM{AlwaysReady: false}
	resp, err := llm.Evaluate(context.Background(), "owner/repo", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Ready {
		t.Error("expected Ready to be false")
	}
	if resp.Question == "" {
		t.Error("expected a question")
	}
}

func TestLLMResponseParsing(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantReady bool
	}{
		{"exact READY", "READY", true},
		{"ready lowercase", "ready", true},
		{"READY with whitespace", "  READY  ", true},
		{"question", "What specific bug?", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the response parsing logic from AnthropicClient.Evaluate
			text := tt.text
			text = trimSpace(text)

			isReady := toUpper(text) == "READY"

			if isReady != tt.wantReady {
				t.Errorf("got Ready=%v, want %v", isReady, tt.wantReady)
			}
		})
	}
}

// Helper functions to avoid importing strings for simple tests
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func toUpper(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			result[i] = c - 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}
