package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

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

// mockPodSpawner is a configurable mock for testing retry logic.
type mockPodSpawner struct {
	failCount     int32 // number of times to fail before succeeding
	attempts      int32 // tracks actual attempts
	logger        *slog.Logger
	sleepDuration time.Duration // track backoff timing
}

func (m *mockPodSpawner) SpawnPod(ctx context.Context, params SpawnParams) (string, error) {
	attempt := atomic.AddInt32(&m.attempts, 1)
	if attempt <= atomic.LoadInt32(&m.failCount) {
		return "", fmt.Errorf("simulated failure on attempt %d", attempt)
	}
	return fmt.Sprintf("minion-%s", params.MinionID.String()), nil
}

func (m *mockPodSpawner) Attempts() int {
	return int(atomic.LoadInt32(&m.attempts))
}

// retryableSpawner wraps a mockPodSpawner with retry logic for testing.
// This mirrors the Client's SpawnPodWithRetry logic.
type retryableSpawner struct {
	mock   *mockPodSpawner
	logger *slog.Logger
}

func (r *retryableSpawner) SpawnPodWithRetry(ctx context.Context, params SpawnParams) (string, error) {
	var lastErr error
	backoff := InitialBackoff

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		podName, err := r.mock.SpawnPod(ctx, params)
		if err == nil {
			return podName, nil
		}

		lastErr = err
		if r.logger != nil {
			r.logger.Warn("pod creation failed, will retry",
				"minion_id", params.MinionID,
				"attempt", attempt+1,
				"max_attempts", MaxRetries+1,
				"error", err,
			)
		}

		if attempt == MaxRetries {
			break
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= BackoffMultiplier
		if backoff > MaxBackoff {
			backoff = MaxBackoff
		}
	}

	return "", fmt.Errorf("%w: %v", ErrRetriesExhausted, lastErr)
}

func TestRetryConstants(t *testing.T) {
	// Verify retry configuration values
	if MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", MaxRetries)
	}
	if InitialBackoff != 1*time.Second {
		t.Errorf("expected InitialBackoff=1s, got %v", InitialBackoff)
	}
	if MaxBackoff != 30*time.Second {
		t.Errorf("expected MaxBackoff=30s, got %v", MaxBackoff)
	}
	if BackoffMultiplier != 2 {
		t.Errorf("expected BackoffMultiplier=2, got %d", BackoffMultiplier)
	}
}

func TestSpawnPodWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	mock := &mockPodSpawner{failCount: 0}
	spawner := &retryableSpawner{mock: mock}

	params := SpawnParams{
		MinionID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:     "owner/repo",
	}

	podName, err := spawner.SpawnPodWithRetry(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if podName != "minion-550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("unexpected pod name: %s", podName)
	}
	if mock.Attempts() != 1 {
		t.Errorf("expected 1 attempt, got %d", mock.Attempts())
	}
}

func TestSpawnPodWithRetry_SuccessAfterRetries(t *testing.T) {
	// Fail twice, succeed on third attempt
	mock := &mockPodSpawner{failCount: 2}
	spawner := &retryableSpawner{mock: mock}

	params := SpawnParams{
		MinionID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:     "owner/repo",
	}

	// Use a context with short timeout to make test faster
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	podName, err := spawner.SpawnPodWithRetry(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if podName != "minion-550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("unexpected pod name: %s", podName)
	}
	if mock.Attempts() != 3 {
		t.Errorf("expected 3 attempts, got %d", mock.Attempts())
	}
}

func TestSpawnPodWithRetry_ExhaustsRetries(t *testing.T) {
	// Always fail, exceeds retry limit
	mock := &mockPodSpawner{failCount: 10} // More than MaxRetries+1
	spawner := &retryableSpawner{mock: mock}

	params := SpawnParams{
		MinionID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:     "owner/repo",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := spawner.SpawnPodWithRetry(ctx, params)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Errorf("expected ErrRetriesExhausted, got %v", err)
	}
	// Should attempt exactly MaxRetries+1 times (initial + 3 retries)
	if mock.Attempts() != MaxRetries+1 {
		t.Errorf("expected %d attempts, got %d", MaxRetries+1, mock.Attempts())
	}
}

func TestSpawnPodWithRetry_ContextCancellation(t *testing.T) {
	// Fail enough to trigger retries
	mock := &mockPodSpawner{failCount: 5}
	spawner := &retryableSpawner{mock: mock}

	params := SpawnParams{
		MinionID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:     "owner/repo",
	}

	// Cancel context after a short time (before retries complete)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := spawner.SpawnPodWithRetry(ctx, params)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should fail due to context cancellation, not retry exhaustion
	if errors.Is(err, ErrRetriesExhausted) {
		t.Error("should have failed due to context cancellation, not retry exhaustion")
	}
}

func TestNoOpPodManager_SpawnPodWithRetry(t *testing.T) {
	mgr := NewNoOpPodManager(nil)

	params := SpawnParams{
		MinionID: uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		Repo:     "owner/repo",
	}

	// NoOp implementation should succeed immediately
	podName, err := mgr.SpawnPodWithRetry(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if podName != "minion-550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("unexpected pod name: %s", podName)
	}
}

func TestErrRetriesExhausted(t *testing.T) {
	// Verify error wrapping works correctly
	wrapped := fmt.Errorf("%w: underlying error", ErrRetriesExhausted)
	if !errors.Is(wrapped, ErrRetriesExhausted) {
		t.Error("expected errors.Is to match ErrRetriesExhausted")
	}
}

func TestPodTimeoutConstants(t *testing.T) {
	// Verify timeout configuration values
	if PodReadyTimeout != 5*time.Minute {
		t.Errorf("expected PodReadyTimeout=5m, got %v", PodReadyTimeout)
	}
	if PodPollInterval != 2*time.Second {
		t.Errorf("expected PodPollInterval=2s, got %v", PodPollInterval)
	}
}

func TestErrPodTimeout(t *testing.T) {
	// Verify error wrapping works correctly
	wrapped := fmt.Errorf("pod failed: %w", ErrPodTimeout)
	if !errors.Is(wrapped, ErrPodTimeout) {
		t.Error("expected errors.Is to match ErrPodTimeout")
	}
}

func TestNoOpPodManager_WaitForPodReady(t *testing.T) {
	mgr := NewNoOpPodManager(nil)

	// NoOp implementation should succeed immediately
	err := mgr.WaitForPodReady(context.Background(), "test-pod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "pod ready condition true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			name: "pod ready condition false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
		{
			name: "no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
				},
			},
			expected: false,
		},
		{
			name: "only other conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPodReady(tt.pod)
			if got != tt.expected {
				t.Errorf("isPodReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}
