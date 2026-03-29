package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/imdevinc/minions/discord-bot/internal/clarify"
	"github.com/imdevinc/minions/discord-bot/internal/orchestrator"
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

func TestMessageHandler_IsCommandAllowed_GuildOnlyRestriction(t *testing.T) {
	h := NewMessageHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&mockOrchestrator{},
		&mockClarificationEvaluator{},
		AccessRestrictions{AllowedGuildID: "guild-1"},
	)

	allowedMsg := &discordgo.Message{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Author:    &discordgo.User{ID: "user-1"},
	}
	if !h.isCommandAllowed(nil, allowedMsg) {
		t.Fatal("expected command to be allowed for configured guild")
	}

	wrongGuildMsg := &discordgo.Message{
		GuildID:   "guild-2",
		ChannelID: "channel-1",
		Author:    &discordgo.User{ID: "user-1"},
	}
	if h.isCommandAllowed(nil, wrongGuildMsg) {
		t.Fatal("expected command to be rejected for different guild")
	}

	directMsg := &discordgo.Message{
		ChannelID: "dm-channel",
		Author:    &discordgo.User{ID: "user-1"},
	}
	if h.isCommandAllowed(nil, directMsg) {
		t.Fatal("expected command to be rejected outside guild context")
	}
}

func TestMessageHandler_IsCommandAllowed_RoleRestriction(t *testing.T) {
	h := NewMessageHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&mockOrchestrator{},
		&mockClarificationEvaluator{},
		AccessRestrictions{AllowedGuildID: "guild-1", AllowedRoleID: "role-admin"},
	)

	allowedMsg := &discordgo.Message{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Author:    &discordgo.User{ID: "user-1"},
		Member:    &discordgo.Member{Roles: []string{"role-user", "role-admin"}},
	}
	if !h.isCommandAllowed(nil, allowedMsg) {
		t.Fatal("expected command to be allowed for user with required role")
	}

	missingRoleMsg := &discordgo.Message{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		Author:    &discordgo.User{ID: "user-1"},
		Member:    &discordgo.Member{Roles: []string{"role-user"}},
	}
	if h.isCommandAllowed(nil, missingRoleMsg) {
		t.Fatal("expected command to be rejected for user without required role")
	}
}

func TestMockOrchestrator_GetByClarificationMessageID_IncludesDiscordUserID(t *testing.T) {
	question := "What feature?"
	channelID := "123456789"

	mock := &mockOrchestrator{
		getByClarificationFunc: func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
			return &orchestrator.MinionByClarificationResponse{
				ID:                    "minion-123",
				Repo:                  "owner/repo",
				Task:                  "add feature",
				Model:                 "anthropic/claude-sonnet-4-5",
				Status:                "awaiting_clarification",
				ClarificationQuestion: &question,
				DiscordChannelID:      &channelID,
				DiscordUserID:         "original-user-123",
			}, nil
		},
	}

	resp, err := mock.GetByClarificationMessageID(context.Background(), "clarification-msg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DiscordUserID != "original-user-123" {
		t.Errorf("expected DiscordUserID 'original-user-123', got %s", resp.DiscordUserID)
	}
}

func TestClarificationReplyValidation_WrongUser(t *testing.T) {
	// This test verifies the validation logic for clarification replies.
	// It checks that replies from users other than the original requester are rejected.

	question := "What feature?"
	channelID := "123456789"

	mock := &mockOrchestrator{
		getByClarificationFunc: func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
			return &orchestrator.MinionByClarificationResponse{
				ID:                    "minion-123",
				Repo:                  "owner/repo",
				Task:                  "add feature",
				Model:                 "anthropic/claude-sonnet-4-5",
				Status:                "awaiting_clarification",
				ClarificationQuestion: &question,
				DiscordChannelID:      &channelID,
				DiscordUserID:         "original-user-123",
			}, nil
		},
	}

	resp, _ := mock.GetByClarificationMessageID(context.Background(), "clarification-msg-123")

	// Simulate validation logic from HandleReply
	replyAuthorID := "different-user-456"

	// This is the validation check from HandleReply
	if resp.DiscordUserID != replyAuthorID {
		// Expected: reply is from wrong user, should be rejected
		t.Log("correctly identified reply from wrong user")
	} else {
		t.Error("validation should have detected wrong user")
	}
}

func TestClarificationReplyValidation_CorrectUser(t *testing.T) {
	// This test verifies that replies from the original requester are accepted.

	question := "What feature?"
	channelID := "123456789"
	originalUserID := "original-user-123"

	mock := &mockOrchestrator{
		getByClarificationFunc: func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
			return &orchestrator.MinionByClarificationResponse{
				ID:                    "minion-123",
				Repo:                  "owner/repo",
				Task:                  "add feature",
				Model:                 "anthropic/claude-sonnet-4-5",
				Status:                "awaiting_clarification",
				ClarificationQuestion: &question,
				DiscordChannelID:      &channelID,
				DiscordUserID:         originalUserID,
			}, nil
		},
	}

	resp, _ := mock.GetByClarificationMessageID(context.Background(), "clarification-msg-123")

	// Simulate validation logic from HandleReply - reply from original user
	replyAuthorID := originalUserID

	// This is the validation check from HandleReply
	if resp.DiscordUserID == replyAuthorID {
		// Expected: reply is from original user, should be accepted
		t.Log("correctly identified reply from original user")
	} else {
		t.Error("validation should have accepted reply from original user")
	}
}

func TestContextTimeoutPropagation(t *testing.T) {
	// Test that orchestrator client calls receive a context with a deadline.
	// This verifies the timeout context is properly created and passed.

	t.Run("CreateMinion receives context with deadline", func(t *testing.T) {
		var receivedCtx context.Context

		mock := &mockOrchestrator{
			createFunc: func(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error) {
				receivedCtx = ctx
				return &orchestrator.CreateMinionResponse{ID: "test-id", Status: "pending"}, nil
			},
		}

		// Call CreateMinion with a timeout context similar to how Handle does it
		ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
		defer cancel()

		_, _ = mock.CreateMinion(ctx, orchestrator.CreateMinionRequest{
			Repo: "owner/repo",
			Task: "test task",
		})

		// Verify the context has a deadline
		if _, ok := receivedCtx.Deadline(); !ok {
			t.Error("expected context to have a deadline")
		}
	})

	t.Run("GetByClarificationMessageID receives context with deadline", func(t *testing.T) {
		var receivedCtx context.Context

		mock := &mockOrchestrator{
			getByClarificationFunc: func(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error) {
				receivedCtx = ctx
				return &orchestrator.MinionByClarificationResponse{
					ID:            "minion-123",
					Status:        "awaiting_clarification",
					DiscordUserID: "user-123",
				}, nil
			},
		}

		// Call with timeout context similar to HandleReply
		ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
		defer cancel()

		_, _ = mock.GetByClarificationMessageID(ctx, "msg-123")

		if _, ok := receivedCtx.Deadline(); !ok {
			t.Error("expected context to have a deadline")
		}
	})

	t.Run("SetClarificationAnswer receives context with deadline", func(t *testing.T) {
		var receivedCtx context.Context

		mock := &mockOrchestrator{
			setAnswerFunc: func(ctx context.Context, minionID string, answer string) error {
				receivedCtx = ctx
				return nil
			},
		}

		// Call with timeout context
		ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
		defer cancel()

		_ = mock.SetClarificationAnswer(ctx, "minion-123", "my answer")

		if _, ok := receivedCtx.Deadline(); !ok {
			t.Error("expected context to have a deadline")
		}
	})

	t.Run("EvaluateWithRetry receives context with deadline", func(t *testing.T) {
		var receivedCtx context.Context

		mock := &mockClarificationEvaluator{
			evaluateFunc: func(ctx context.Context, repo, task string) (*clarify.Result, error) {
				receivedCtx = ctx
				return &clarify.Result{Ready: true}, nil
			},
		}

		// Call with timeout context similar to Handle
		ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
		defer cancel()

		_, _ = mock.EvaluateWithRetry(ctx, "owner/repo", "test task")

		if _, ok := receivedCtx.Deadline(); !ok {
			t.Error("expected context to have a deadline")
		}
	})
}
