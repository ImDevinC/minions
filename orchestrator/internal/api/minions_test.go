package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
	"github.com/go-chi/chi/v5"
)

func newTestHandler(t *testing.T) *MinionHandler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return NewMinionHandler(MinionHandlerConfig{
		Users:         nil,
		Minions:       nil,
		Events:        nil,
		PodTerminator: k8s.NewNoOpPodTerminator(logger),
		Notifier:      webhook.NewNoOpNotifier(logger),
		Logger:        logger,
	})
}

func TestMinionHandler_CreateValidation(t *testing.T) {
	// These tests verify request validation logic (no DB needed, will fail on DB call)
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		body       CreateMinionRequest
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing required fields",
			body:       CreateMinionRequest{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing repo",
			body: CreateMinionRequest{
				Task:          "fix the bug",
				Model:         "anthropic/claude-sonnet-4-5",
				DiscordUserID: "123456",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid repo format - no slash",
			body: CreateMinionRequest{
				Repo:          "invalid-repo",
				Task:          "fix the bug",
				Model:         "anthropic/claude-sonnet-4-5",
				DiscordUserID: "123456",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_REPO",
		},
		{
			name: "invalid repo format - starts with slash",
			body: CreateMinionRequest{
				Repo:          "/owner/repo",
				Task:          "fix the bug",
				Model:         "anthropic/claude-sonnet-4-5",
				DiscordUserID: "123456",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_REPO",
		},
		{
			name: "invalid repo format - invalid characters",
			body: CreateMinionRequest{
				Repo:          "owner/repo with spaces",
				Task:          "fix the bug",
				Model:         "anthropic/claude-sonnet-4-5",
				DiscordUserID: "123456",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_REPO",
		},
		{
			name: "task exceeds max length",
			body: CreateMinionRequest{
				Repo:          "owner/repo",
				Task:          string(make([]byte, MaxTaskLength+1)),
				Model:         "anthropic/claude-sonnet-4-5",
				DiscordUserID: "123456",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "TASK_TOO_LONG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/minions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.HandleCreate(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantCode != "" {
				var resp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Code != tt.wantCode {
					t.Errorf("expected code %q, got %q", tt.wantCode, resp.Code)
				}
			}
		})
	}
}

func TestRepoRegex(t *testing.T) {
	validRepos := []string{
		"owner/repo",
		"my-org/my-repo",
		"my_org/my_repo",
		"org/repo-name",
		"org/repo.name",
		"GitOrg/GitHub-Repo",
		"org/repo/subgroup", // nested/monorepo
		"gitlab-group/subgroup/project",
	}

	invalidRepos := []string{
		"invalid",
		"/owner/repo",
		"owner/repo/",
		"owner//repo",
		"owner/repo with spaces",
		"owner/repo@version",
		"",
		"/",
		"//",
	}

	for _, repo := range validRepos {
		if !repoRegex.MatchString(repo) {
			t.Errorf("expected %q to be valid, but it was rejected", repo)
		}
	}

	for _, repo := range invalidRepos {
		if repoRegex.MatchString(repo) {
			t.Errorf("expected %q to be invalid, but it was accepted", repo)
		}
	}
}

func TestMinionHandler_GetValidation(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		id         string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid uuid format",
			id:         "not-a-uuid",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "empty id",
			id:         "",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "partial uuid",
			id:         "550e8400-e29b",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/minions/"+tt.id, nil)

			// chi router context needed for URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			handler.HandleGet(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantCode != "" {
				var resp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Code != tt.wantCode {
					t.Errorf("expected code %q, got %q", tt.wantCode, resp.Code)
				}
			}
		})
	}
}

func TestMinionHandler_DeleteValidation(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		id         string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid uuid format",
			id:         "not-a-uuid",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "empty id",
			id:         "",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "partial uuid",
			id:         "550e8400-e29b",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/minions/"+tt.id, nil)

			// chi router context needed for URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			handler.HandleDelete(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantCode != "" {
				var resp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Code != tt.wantCode {
					t.Errorf("expected code %q, got %q", tt.wantCode, resp.Code)
				}
			}
		})
	}
}

func TestMinionHandler_CallbackValidation(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		id         string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid uuid format",
			id:         "not-a-uuid",
			body:       CallbackRequest{Status: "completed"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "empty id",
			id:         "",
			body:       CallbackRequest{Status: "completed"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "invalid json body",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid status value",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       CallbackRequest{Status: "running"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_STATUS",
		},
		{
			name:       "empty status",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       CallbackRequest{Status: ""},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_STATUS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			switch v := tt.body.(type) {
			case string:
				body = []byte(v)
			default:
				body, _ = json.Marshal(tt.body)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/minions/"+tt.id+"/callback", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			// chi router context needed for URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			handler.HandleCallback(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantCode != "" {
				var resp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Code != tt.wantCode {
					t.Errorf("expected code %q, got %q", tt.wantCode, resp.Code)
				}
			}
		})
	}
}

func TestMinionHandler_ClarificationValidation(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		id         string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid uuid format",
			id:         "not-a-uuid",
			body:       ClarificationRequest{Question: "What's the scope?", DiscordMessageID: "123"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "empty id",
			id:         "",
			body:       ClarificationRequest{Question: "What's the scope?", DiscordMessageID: "123"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ID",
		},
		{
			name:       "invalid json body",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing question",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       ClarificationRequest{Question: "", DiscordMessageID: "123"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "MISSING_QUESTION",
		},
		{
			name:       "missing discord_message_id",
			id:         "550e8400-e29b-41d4-a716-446655440000",
			body:       ClarificationRequest{Question: "What's the scope?", DiscordMessageID: ""},
			wantStatus: http.StatusBadRequest,
			wantCode:   "MISSING_DISCORD_MESSAGE_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			switch v := tt.body.(type) {
			case string:
				body = []byte(v)
			default:
				body, _ = json.Marshal(tt.body)
			}
			req := httptest.NewRequest(http.MethodPatch, "/api/minions/"+tt.id+"/clarification", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			// chi router context needed for URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			handler.HandleClarification(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantCode != "" {
				var resp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Code != tt.wantCode {
					t.Errorf("expected code %q, got %q", tt.wantCode, resp.Code)
				}
			}
		})
	}
}

func TestMinionHandler_RequestBodyLimit(t *testing.T) {
	handler := newTestHandler(t)

	t.Run("rejects body exceeding 1MB", func(t *testing.T) {
		// Create a body that's just over 1MB
		largeBody := make([]byte, MaxRequestBodySize+1)
		for i := range largeBody {
			largeBody[i] = 'a'
		}

		req := httptest.NewRequest(http.MethodPost, "/api/minions", bytes.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.HandleCreate(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("accepts body under 1MB", func(t *testing.T) {
		// Create a valid request body under 1MB
		body := CreateMinionRequest{
			Repo:          "owner/repo",
			Task:          "fix the bug",
			Model:         "anthropic/claude-sonnet-4-5",
			DiscordUserID: "123456",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/minions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Handler will panic on DB call since we have no DB - that's expected
		// A panic means the body was successfully read and validated
		defer func() {
			if r := recover(); r == nil {
				// No panic - check the response
				if rr.Code == http.StatusBadRequest {
					var resp ErrorResponse
					if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
						t.Fatalf("failed to unmarshal response: %v", err)
					}
					if resp.Error == "invalid request body" {
						t.Errorf("valid body was rejected as invalid")
					}
				}
			}
			// Panic occurred - this means body was accepted and we proceeded to DB ops
		}()

		handler.HandleCreate(rr, req)
	})
}

func TestMinionHandler_TaskLengthLimit(t *testing.T) {
	handler := newTestHandler(t)

	t.Run("accepts task at exactly max length", func(t *testing.T) {
		// Task of exactly MaxTaskLength chars should be accepted
		body := CreateMinionRequest{
			Repo:          "owner/repo",
			Task:          string(make([]byte, MaxTaskLength)),
			Model:         "anthropic/claude-sonnet-4-5",
			DiscordUserID: "123456",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/minions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Handler will panic on DB call since we have no DB - that's expected
		// A panic means the body was successfully read, validated, and task length passed
		defer func() {
			if r := recover(); r == nil {
				// No panic - check we didn't get TASK_TOO_LONG error
				if rr.Code == http.StatusBadRequest {
					var resp ErrorResponse
					if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
						t.Fatalf("failed to unmarshal response: %v", err)
					}
					if resp.Code == "TASK_TOO_LONG" {
						t.Errorf("task at exactly max length was incorrectly rejected")
					}
				}
			}
			// Panic occurred - validation passed, proceeded to DB ops
		}()

		handler.HandleCreate(rr, req)
	})
}
