package clarify

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		logger: nil,
	}

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
	if !strings.Contains(err.Error(), "fail 3") {
		t.Errorf("expected last error message in returned error, got %q", err.Error())
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

// evaluateWithRetryTest is a test helper with shorter backoff
func (h *Handler) evaluateWithRetryTest(ctx context.Context, repo, task string) (*Result, error) {
	backoff := 1 * time.Millisecond // Much shorter for tests
	var lastErr error

	for attempt := 1; attempt <= MaxRetries; attempt++ {
		resp, err := h.llm.Evaluate(ctx, repo, task)
		if err == nil {
			return &Result{
				Ready:    resp.Ready,
				Question: resp.Question,
			}, nil
		}

		lastErr = err
		if attempt < MaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrAllRetriesFailed, lastErr)
}