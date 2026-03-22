package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRouter_HealthNoAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	router := NewRouter(logger, "test-token")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestRouter_ApiRequiresAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	router := NewRouter(logger, "test-token")

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Without auth, should get 401
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestRouter_ApiWithValidAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	token := "test-token"
	router := NewRouter(logger, token)

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// With valid auth, should get 200 with "pong"
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.String() != "pong" {
		t.Errorf("expected body 'pong', got %q", rr.Body.String())
	}
}
