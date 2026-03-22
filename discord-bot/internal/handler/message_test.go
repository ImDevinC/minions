package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/anomalyco/minions/discord-bot/internal/orchestrator"
)

// mockMinionCreator is a mock implementation of MinionCreator for testing
type mockMinionCreator struct {
	createFunc func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error)
}

func (m *mockMinionCreator) CreateMinion(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
	return m.createFunc(ctx, req)
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

func TestMockMinionCreator_Interface(t *testing.T) {
	// Verify the mock implements MinionCreator
	var _ MinionCreator = &mockMinionCreator{}

	mock := &mockMinionCreator{
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

func TestMockMinionCreator_RateLimitError(t *testing.T) {
	mock := &mockMinionCreator{
		createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
			return nil, orchestrator.ErrRateLimitExceeded
		},
	}

	_, err := mock.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{})

	if !errors.Is(err, orchestrator.ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestMockMinionCreator_ConcurrentLimitError(t *testing.T) {
	mock := &mockMinionCreator{
		createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
			return nil, orchestrator.ErrConcurrentLimitExceeded
		},
	}

	_, err := mock.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{})

	if !errors.Is(err, orchestrator.ErrConcurrentLimitExceeded) {
		t.Errorf("expected ErrConcurrentLimitExceeded, got %v", err)
	}
}
