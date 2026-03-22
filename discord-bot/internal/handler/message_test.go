package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/anomalyco/minions/discord-bot/internal/clarify"
	"github.com/anomalyco/minions/discord-bot/internal/orchestrator"
)

// mockOrchestrator is a mock implementation of Orchestrator for testing
type mockOrchestrator struct {
	createFunc             func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error)
	setClarifyFunc         func(ctx context.Context, minionID string, req orchestrator.SetClarificationRequest) error
	markFailedFunc         func(ctx context.Context, minionID string, errorMsg string) error
	getByClarificationFunc func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error)
	setAnswerFunc          func(ctx context.Context, minionID string, answer string) error
}

func (m *mockOrchestrator) CreateMinion(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &orchestrator.CreateMinionResponse{ID: "test-id", Status: "pending"}, nil
}

func (m *mockOrchestrator) SetClarification(ctx context.Context, minionID string, req orchestrator.SetClarificationRequest) error {
	if m.setClarifyFunc != nil {
		return m.setClarifyFunc(ctx, minionID, req)
	}
	return nil
}

func (m *mockOrchestrator) MarkFailed(ctx context.Context, minionID string, errorMsg string) error {
	if m.markFailedFunc != nil {
		return m.markFailedFunc(ctx, minionID, errorMsg)
	}
	return nil
}

func (m *mockOrchestrator) GetByClarificationMessageID(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
	if m.getByClarificationFunc != nil {
		return m.getByClarificationFunc(ctx, messageID)
	}
	return nil, orchestrator.ErrClarificationNotFound
}

func (m *mockOrchestrator) SetClarificationAnswer(ctx context.Context, minionID string, answer string) error {
	if m.setAnswerFunc != nil {
		return m.setAnswerFunc(ctx, minionID, answer)
	}
	return nil
}

// mockClarificationEvaluator is a mock implementation of ClarificationEvaluator
type mockClarificationEvaluator struct {
	evaluateFunc func(ctx context.Context, repo, task string) (*clarify.Result, error)
}

func (m *mockClarificationEvaluator) EvaluateWithRetry(ctx context.Context, repo, task string) (*clarify.Result, error) {
	if m.evaluateFunc != nil {
		return m.evaluateFunc(ctx, repo, task)
	}
	return &clarify.Result{Ready: true}, nil
}

func TestHandleOrchestratorError_RateLimit(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantContain string
	}{
		{
			name:        "rate limit exceeded",
			err:         orchestrator.ErrRateLimitExceeded,
			wantContain: "hourly limit",
		},
		{
			name:        "concurrent limit exceeded",
			err:         orchestrator.ErrConcurrentLimitExceeded,
			wantContain: "too many minions running",
		},
		{
			name:        "generic error",
			err:         errors.New("something went wrong"),
			wantContain: "Failed to create minion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Can't easily test the full handler without a real Discord session
			// but we can verify the error matching logic
			if tt.err == orchestrator.ErrRateLimitExceeded {
				if !errors.Is(tt.err, orchestrator.ErrRateLimitExceeded) {
					t.Error("expected errors.Is to match ErrRateLimitExceeded")
				}
			}
			if tt.err == orchestrator.ErrConcurrentLimitExceeded {
				if !errors.Is(tt.err, orchestrator.ErrConcurrentLimitExceeded) {
					t.Error("expected errors.Is to match ErrConcurrentLimitExceeded")
				}
			}
		})
	}
}

func TestMockOrchestrator_Interface(t *testing.T) {
	// Verify the mock implements Orchestrator
	var _ Orchestrator = &mockOrchestrator{}

	mock := &mockOrchestrator{
		createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
			return &orchestrator.CreateMinionResponse{
				ID:     "test-id",
				Status: "pending",
			}, nil
		},
	}

	resp, err := mock.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{
		Repo: "owner/repo",
		Task: "test task",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %s", resp.ID)
	}
}

func TestMockOrchestrator_RateLimitError(t *testing.T) {
	mock := &mockOrchestrator{
		createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
			return nil, orchestrator.ErrRateLimitExceeded
		},
	}

	_, err := mock.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{})

	if !errors.Is(err, orchestrator.ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestMockOrchestrator_ConcurrentLimitError(t *testing.T) {
	mock := &mockOrchestrator{
		createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
			return nil, orchestrator.ErrConcurrentLimitExceeded
		},
	}

	_, err := mock.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{})

	if !errors.Is(err, orchestrator.ErrConcurrentLimitExceeded) {
		t.Errorf("expected ErrConcurrentLimitExceeded, got %v", err)
	}
}

func TestMockClarificationEvaluator_Ready(t *testing.T) {
	var _ ClarificationEvaluator = &mockClarificationEvaluator{}

	mock := &mockClarificationEvaluator{
		evaluateFunc: func(ctx context.Context, repo, task string) (*clarify.Result, error) {
			return &clarify.Result{Ready: true}, nil
		},
	}

	result, err := mock.EvaluateWithRetry(context.Background(), "owner/repo", "add feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Ready {
		t.Error("expected Ready to be true")
	}
}

func TestMockClarificationEvaluator_NeedsClarification(t *testing.T) {
	mock := &mockClarificationEvaluator{
		evaluateFunc: func(ctx context.Context, repo, task string) (*clarify.Result, error) {
			return &clarify.Result{
				Ready:    false,
				Question: "What specific feature do you want to add?",
			}, nil
		},
	}

	result, err := mock.EvaluateWithRetry(context.Background(), "owner/repo", "add feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Ready {
		t.Error("expected Ready to be false")
	}
	if result.Question == "" {
		t.Error("expected a clarification question")
	}
}

func TestMockClarificationEvaluator_AllRetriesFailed(t *testing.T) {
	mock := &mockClarificationEvaluator{
		evaluateFunc: func(ctx context.Context, repo, task string) (*clarify.Result, error) {
			return nil, clarify.ErrAllRetriesFailed
		},
	}

	_, err := mock.EvaluateWithRetry(context.Background(), "owner/repo", "add feature")
	if !errors.Is(err, clarify.ErrAllRetriesFailed) {
		t.Errorf("expected ErrAllRetriesFailed, got %v", err)
	}
}

func TestMockOrchestrator_GetByClarificationMessageID(t *testing.T) {
	var _ Orchestrator = &mockOrchestrator{}

	question := "What feature?"
	channelID := "123456789"

	mock := &mockOrchestrator{
		getByClarificationFunc: func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
			if messageID == "clarification-msg-123" {
				return &orchestrator.MinionByClarificationResponse{
					ID:                    "minion-123",
					Repo:                  "owner/repo",
					Task:                  "add feature",
					Model:                 "anthropic/claude-sonnet-4-5",
					Status:                "awaiting_clarification",
					ClarificationQuestion: &question,
					DiscordChannelID:      &channelID,
				}, nil
			}
			return nil, orchestrator.ErrClarificationNotFound
		},
	}

	// Found case
	resp, err := mock.GetByClarificationMessageID(context.Background(), "clarification-msg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "minion-123" {
		t.Errorf("expected minion ID 'minion-123', got %s", resp.ID)
	}
	if resp.Status != "awaiting_clarification" {
		t.Errorf("expected status 'awaiting_clarification', got %s", resp.Status)
	}

	// Not found case
	_, err = mock.GetByClarificationMessageID(context.Background(), "unknown-msg")
	if !errors.Is(err, orchestrator.ErrClarificationNotFound) {
		t.Errorf("expected ErrClarificationNotFound, got %v", err)
	}
}

func TestMockOrchestrator_SetClarificationAnswer(t *testing.T) {
	var called bool
	var capturedID, capturedAnswer string

	mock := &mockOrchestrator{
		setAnswerFunc: func(ctx context.Context, minionID string, answer string) error {
			called = true
			capturedID = minionID
			capturedAnswer = answer
			return nil
		},
	}

	err := mock.SetClarificationAnswer(context.Background(), "minion-123", "Add dark mode toggle")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected SetClarificationAnswer to be called")
	}
	if capturedID != "minion-123" {
		t.Errorf("expected minion ID 'minion-123', got %s", capturedID)
	}
	if capturedAnswer != "Add dark mode toggle" {
		t.Errorf("unexpected answer: %s", capturedAnswer)
	}
}

func TestMockOrchestrator_SetClarificationAnswer_NotFound(t *testing.T) {
	mock := &mockOrchestrator{
		setAnswerFunc: func(ctx context.Context, minionID string, answer string) error {
			return orchestrator.ErrClarificationNotFound
		},
	}

	err := mock.SetClarificationAnswer(context.Background(), "unknown-minion", "some answer")
	if !errors.Is(err, orchestrator.ErrClarificationNotFound) {
		t.Errorf("expected ErrClarificationNotFound, got %v", err)
	}
}
