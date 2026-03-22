// Package github provides GitHub App token management for the orchestrator.
package github

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

func TestParseRepo(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{
			name:      "valid simple repo",
			repo:      "owner/repo",
			wantOwner: "owner",
			wantName:  "repo",
			wantOK:    true,
		},
		{
			name:      "valid org repo",
			repo:      "anomalyco/minions",
			wantOwner: "anomalyco",
			wantName:  "minions",
			wantOK:    true,
		},
		{
			name:   "empty string",
			repo:   "",
			wantOK: false,
		},
		{
			name:   "no slash",
			repo:   "owner",
			wantOK: false,
		},
		{
			name:   "empty owner",
			repo:   "/repo",
			wantOK: false,
		},
		{
			name:   "empty name",
			repo:   "owner/",
			wantOK: false,
		},
		{
			name:      "nested path (uses first segment as owner)",
			repo:      "owner/repo/subpath",
			wantOwner: "owner",
			wantName:  "repo/subpath",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, name, ok := parseRepo(tt.repo)
			if ok != tt.wantOK {
				t.Errorf("parseRepo(%q) ok = %v, want %v", tt.repo, ok, tt.wantOK)
			}
			if ok {
				if owner != tt.wantOwner {
					t.Errorf("parseRepo(%q) owner = %q, want %q", tt.repo, owner, tt.wantOwner)
				}
				if name != tt.wantName {
					t.Errorf("parseRepo(%q) name = %q, want %q", tt.repo, name, tt.wantName)
				}
			}
		})
	}
}

func TestNewManager_Validation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing app ID",
			cfg:     Config{AppID: 0, PrivateKey: []byte("key")},
			wantErr: "github app ID is required",
		},
		{
			name:    "missing private key",
			cfg:     Config{AppID: 123, PrivateKey: nil},
			wantErr: "github app private key is required",
		},
		{
			name:    "empty private key",
			cfg:     Config{AppID: 123, PrivateKey: []byte{}},
			wantErr: "github app private key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(tt.cfg, logger)
			if err == nil {
				t.Error("expected error, got nil")
				return
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestIsAccessRevokedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "error containing 401",
			err:      errors.New("request failed: 401 Unauthorized"),
			expected: true,
		},
		{
			name:     "error containing 403",
			err:      errors.New("access denied: 403 Forbidden"),
			expected: true,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				// Can't call isAccessRevokedError(nil)
				return
			}
			result := isAccessRevokedError(tt.err)
			if result != tt.expected {
				t.Errorf("isAccessRevokedError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestNoOpTokenManager(t *testing.T) {
	ctx := context.Background()

	t.Run("returns default token", func(t *testing.T) {
		manager := NewNoOpTokenManager(nil, "test-token-123")
		token, err := manager.GetToken(ctx, "owner/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-token-123" {
			t.Errorf("token = %q, want %q", token, "test-token-123")
		}
	})

	t.Run("returns error when no default token", func(t *testing.T) {
		manager := NewNoOpTokenManager(nil, "")
		_, err := manager.GetToken(ctx, "owner/repo")
		if err == nil {
			t.Error("expected error, got nil")
		}
		if err.Error() != "no GitHub token configured" {
			t.Errorf("error = %q, want %q", err.Error(), "no GitHub token configured")
		}
	})
}

func TestErrAccessRevoked(t *testing.T) {
	// Verify the error can be used with errors.Is
	err := ErrAccessRevoked
	if !errors.Is(err, ErrAccessRevoked) {
		t.Error("errors.Is(ErrAccessRevoked, ErrAccessRevoked) should be true")
	}
}

func TestErrInstallationNotFound(t *testing.T) {
	// Verify the error can be used with errors.Is
	err := ErrInstallationNotFound
	if !errors.Is(err, ErrInstallationNotFound) {
		t.Error("errors.Is(ErrInstallationNotFound, ErrInstallationNotFound) should be true")
	}
}
