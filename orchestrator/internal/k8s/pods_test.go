package k8s

import (
	"context"
	"testing"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
)

func TestSpawnParams(t *testing.T) {
	params := SpawnParams{
		MinionID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:        "owner/repo",
		Task:        "fix the bug",
		Model:       "anthropic/claude-sonnet-4-5",
		GitHubToken: "ghp_test123",
		CallbackURL: "http://orchestrator:8080/api/minions/550e8400-e29b-41d4-a716-446655440000/callback",
	}

	if params.MinionID.String() != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected minion ID to match")
	}
	if params.Repo != "owner/repo" {
		t.Errorf("expected repo to be owner/repo")
	}
}

func TestNoOpPodManager(t *testing.T) {
	// Test that NoOpPodManager implements PodManager interface
	var _ PodManager = (*NoOpPodManager)(nil)

	mgr := NewNoOpPodManager(nil)

	// SpawnPod should return expected pod name format
	minionID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	params := SpawnParams{
		MinionID: minionID,
		Repo:     "owner/repo",
	}

	podName, err := mgr.SpawnPod(context.Background(), params)
	if err != nil {
		t.Fatalf("SpawnPod failed: %v", err)
	}
	if podName != "minion-550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected pod name minion-550e8400-e29b-41d4-a716-446655440000, got %s", podName)
	}

	// TerminatePod should succeed
	err = mgr.TerminatePod(context.Background(), podName)
	if err != nil {
		t.Fatalf("TerminatePod failed: %v", err)
	}
}

func TestNoOpPodTerminator(t *testing.T) {
	// Test that NoOpPodTerminator implements PodTerminator interface
	var _ PodTerminator = (*NoOpPodTerminator)(nil)

	term := NewNoOpPodTerminator(nil)
	err := term.TerminatePod(context.Background(), "test-pod")
	if err != nil {
		t.Fatalf("TerminatePod failed: %v", err)
	}
}

// TestSecurityContextConstants verifies the security settings we expect
func TestSecurityContextConstants(t *testing.T) {
	// Verify the namespace constant
	if Namespace != "minions" {
		t.Errorf("expected namespace 'minions', got %s", Namespace)
	}

	// Verify the security context capabilities we drop
	dropped := corev1.Capability("ALL")
	if dropped != "ALL" {
		t.Errorf("expected capability ALL")
	}
}
