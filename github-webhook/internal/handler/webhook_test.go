package handler

import (
	"log/slog"
	"testing"
)

// newTestHandler creates a WebhookHandler with the given approved repos and a
// discard logger so tests don't produce output.
func newTestHandler(approvedRepos map[string]bool) *WebhookHandler {
	return NewWebhookHandler(Config{
		ApprovedRepos: approvedRepos,
		Logger:        slog.New(slog.DiscardHandler),
	})
}

func TestIsRepoApproved_ExactMatch(t *testing.T) {
	h := newTestHandler(map[string]bool{
		"imdevinc/minions": true,
		"myorg/backend":    true,
	})

	tests := []struct {
		name string
		repo string
		want bool
	}{
		{"exact match", "imdevinc/minions", true},
		{"exact match other repo", "myorg/backend", true},
		{"case insensitive repo name", "ImDevinC/Minions", true},
		{"case insensitive owner", "IMDEVINC/minions", true},
		{"unlisted repo rejected", "imdevinc/other", false},
		{"unlisted owner rejected", "otherorg/minions", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isRepoApproved(tt.repo); got != tt.want {
				t.Fatalf("isRepoApproved(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}

func TestIsRepoApproved_OwnerOnly(t *testing.T) {
	h := newTestHandler(map[string]bool{
		"imdevinc": true,
	})

	tests := []struct {
		name string
		repo string
		want bool
	}{
		{"all repos from owner allowed", "imdevinc/minions", true},
		{"all repos from owner allowed", "imdevinc/anything-else", true},
		{"case insensitive owner", "ImDevinC/minions", true},
		{"other owner rejected", "otherorg/minions", false},
		{"other owner rejected even if similar", "imdevinc2/minions", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isRepoApproved(tt.repo); got != tt.want {
				t.Fatalf("isRepoApproved(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}

func TestIsRepoApproved_Wildcard(t *testing.T) {
	h := newTestHandler(map[string]bool{
		"imdevinc/*": true,
	})

	tests := []struct {
		name string
		repo string
		want bool
	}{
		{"wildcard allows any repo from owner", "imdevinc/minions", true},
		{"wildcard allows any repo from owner", "imdevinc/another-repo", true},
		{"case insensitive owner wildcard", "ImDevinC/Minions", true},
		{"other owner rejected", "otherorg/minions", false},
		{"owner prefix collision rejected", "imdevinc2/minions", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isRepoApproved(tt.repo); got != tt.want {
				t.Fatalf("isRepoApproved(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}

func TestIsRepoApproved_MixedFormats(t *testing.T) {
	// Existing allowlist behavior must continue to work alongside the new
	// owner/org and wildcard entries.
	h := newTestHandler(map[string]bool{
		"myorg/backend-api": true, // exact repo (existing behavior)
		"myorg":             true, // owner/org entry
		"otherorg/*":        true, // wildcard entry
	})

	tests := []struct {
		name string
		repo string
		want bool
	}{
		{"exact repo still allowed", "myorg/backend-api", true},
		{"exact repo case insensitive", "MYORG/Backend-API", true},
		{"owner entry allows unlisted repo", "myorg/new-repo", true},
		{"wildcard entry allows repo", "otherorg/anything", true},
		{"unrelated owner rejected", "thirdorg/something", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isRepoApproved(tt.repo); got != tt.want {
				t.Fatalf("isRepoApproved(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}

func TestIsRepoApproved_InvalidRepo(t *testing.T) {
	h := newTestHandler(map[string]bool{
		"imdevinc": true,
	})

	// Malformed repo strings should never be allowed.
	for _, repo := range []string{"", "no-slash", "/", "owner/", "/repo"} {
		t.Run(repo, func(t *testing.T) {
			if h.isRepoApproved(repo) {
				t.Fatalf("isRepoApproved(%q) = true, want false", repo)
			}
		})
	}
}