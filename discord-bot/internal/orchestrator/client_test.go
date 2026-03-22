package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_CreateMinion_Success(t *testing.T) {
	resp := CreateMinionResponse{
		ID:        "123e4567-e89b-12d3-a456-426614174000",
		Status:    "pending",
		Repo:      "owner/repo",
		Task:      "fix the bug",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/minions" {
			t.Errorf("expected /api/minions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	result, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo:             "owner/repo",
		Task:             "fix the bug",
		Model:            "anthropic/claude-sonnet-4-5",
		DiscordUserID:    "user123",
		DiscordUsername:  "testuser",
		DiscordChannelID: "channel123",
		DiscordMessageID: "message123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != resp.ID {
		t.Errorf("expected ID %s, got %s", resp.ID, result.ID)
	}
	if result.Duplicate {
		t.Error("expected Duplicate=false")
	}
}

func TestClient_CreateMinion_Duplicate(t *testing.T) {
	resp := CreateMinionResponse{
		ID:        "123e4567-e89b-12d3-a456-426614174000",
		Status:    "pending",
		Repo:      "owner/repo",
		Task:      "fix the bug",
		CreatedAt: "2024-01-01T00:00:00Z",
		Duplicate: true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 for duplicate
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	result, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo: "owner/repo",
		Task: "fix the bug",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Duplicate {
		t.Error("expected Duplicate=true")
	}
}

func TestClient_CreateMinion_RateLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(ErrorResponse{
			Error: "rate limit exceeded",
			Code:  "RATE_LIMIT_EXCEEDED",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo: "owner/repo",
		Task: "fix the bug",
	})

	if err != ErrRateLimitExceeded {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestClient_CreateMinion_ConcurrentLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(ErrorResponse{
			Error: "concurrent limit exceeded",
			Code:  "CONCURRENT_LIMIT_EXCEEDED",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo: "owner/repo",
		Task: "fix the bug",
	})

	if err != ErrConcurrentLimitExceeded {
		t.Errorf("expected ErrConcurrentLimitExceeded, got %v", err)
	}
}

func TestClient_CreateMinion_BadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{
			Error: "invalid repo format",
			Code:  "INVALID_REPO",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo: "bad-repo",
		Task: "fix the bug",
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_CreateMinion_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.CreateMinion(context.Background(), CreateMinionRequest{
		Repo: "owner/repo",
		Task: "fix the bug",
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
}
