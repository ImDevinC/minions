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

	"github.com/go-chi/chi/v5"
)

func TestMinionHandler_CreateValidation(t *testing.T) {
	// These tests verify request validation logic (no DB needed, will fail on DB call)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	handler := NewMinionHandler(nil, nil, nil, logger)

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
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	handler := NewMinionHandler(nil, nil, nil, logger)

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
