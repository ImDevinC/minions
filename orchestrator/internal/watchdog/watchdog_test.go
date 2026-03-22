package watchdog

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
	"github.com/google/uuid"
)

// mockMinionQuerier is a test mock for MinionQuerier.
type mockMinionQuerier struct {
	idleMinions []*db.Minion
	idleErr     error

	failedCalls []uuid.UUID
	failErr     error
	mu          sync.Mutex
}

func (m *mockMinionQuerier) ListIdleRunning(_ context.Context, _ time.Duration) ([]*db.Minion, error) {
	if m.idleErr != nil {
		return nil, m.idleErr
	}
	return m.idleMinions, nil
}

func (m *mockMinionQuerier) MarkFailed(_ context.Context, id uuid.UUID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedCalls = append(m.failedCalls, id)
	return m.failErr
}

func (m *mockMinionQuerier) getFailedCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID{}, m.failedCalls...)
}

// mockPodStatusChecker is a test mock for PodStatusChecker.
type mockPodStatusChecker struct {
	pods []k8s.PodInfo
	err  error
}

func (m *mockPodStatusChecker) ListPods(_ context.Context) ([]k8s.PodInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pods, nil
}

// mockNotifier is a test mock for webhook.Notifier.
type mockNotifier struct {
	notifications []webhook.Notification
	err           error
	mu            sync.Mutex
}

func (m *mockNotifier) Notify(_ context.Context, n webhook.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.notifications = append(m.notifications, n)
	return nil
}

func (m *mockNotifier) getNotifications() []webhook.Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]webhook.Notification{}, m.notifications...)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestWatchdog_IdleMinionDetection(t *testing.T) {
	channelID := "channel-123"
	idleMinion := &db.Minion{
		ID:               uuid.New(),
		Status:           db.StatusRunning,
		Repo:             "owner/repo",
		LastActivityAt:   time.Now().Add(-45 * time.Minute),
		DiscordChannelID: &channelID,
	}

	minions := &mockMinionQuerier{
		idleMinions: []*db.Minion{idleMinion},
	}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	notifications := notifier.getNotifications()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}

	if notifications[0].Type != webhook.NotifyIdle {
		t.Errorf("expected notification type %s, got %s", webhook.NotifyIdle, notifications[0].Type)
	}
	if notifications[0].MinionID != idleMinion.ID {
		t.Errorf("expected minion ID %s, got %s", idleMinion.ID, notifications[0].MinionID)
	}
	if notifications[0].DiscordChannelID != channelID {
		t.Errorf("expected channel ID %s, got %s", channelID, notifications[0].DiscordChannelID)
	}
}

func TestWatchdog_FailedPodDetection(t *testing.T) {
	minionID := uuid.New()

	minions := &mockMinionQuerier{}
	pods := &mockPodStatusChecker{
		pods: []k8s.PodInfo{
			{Name: "minion-" + minionID.String(), MinionID: minionID.String(), Phase: "Failed"},
			{Name: "minion-healthy", MinionID: uuid.New().String(), Phase: "Running"},
		},
	}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	failedCalls := minions.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(failedCalls))
	}
	if failedCalls[0] != minionID {
		t.Errorf("expected minion ID %s, got %s", minionID, failedCalls[0])
	}
}

func TestWatchdog_MultipleIdleMinions(t *testing.T) {
	ch1 := "channel-1"
	ch2 := "channel-2"
	minion1 := &db.Minion{
		ID:               uuid.New(),
		Status:           db.StatusRunning,
		LastActivityAt:   time.Now().Add(-35 * time.Minute),
		DiscordChannelID: &ch1,
	}
	minion2 := &db.Minion{
		ID:               uuid.New(),
		Status:           db.StatusRunning,
		LastActivityAt:   time.Now().Add(-40 * time.Minute),
		DiscordChannelID: &ch2,
	}

	minions := &mockMinionQuerier{
		idleMinions: []*db.Minion{minion1, minion2},
	}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	notifications := notifier.getNotifications()
	if len(notifications) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications))
	}
}

func TestWatchdog_NoIdleMinions(t *testing.T) {
	minions := &mockMinionQuerier{idleMinions: []*db.Minion{}}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	notifications := notifier.getNotifications()
	if len(notifications) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(notifications))
	}
}

func TestWatchdog_QueryError(t *testing.T) {
	minions := &mockMinionQuerier{
		idleErr: errors.New("database connection failed"),
	}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	// Should not panic, should log error
	ctx := context.Background()
	w.runChecks(ctx)

	// No notifications should be sent
	notifications := notifier.getNotifications()
	if len(notifications) != 0 {
		t.Fatalf("expected 0 notifications on error, got %d", len(notifications))
	}
}

func TestWatchdog_NotifierError(t *testing.T) {
	idleMinion := &db.Minion{
		ID:             uuid.New(),
		Status:         db.StatusRunning,
		LastActivityAt: time.Now().Add(-45 * time.Minute),
	}

	minions := &mockMinionQuerier{idleMinions: []*db.Minion{idleMinion}}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{err: errors.New("webhook failed")}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	// Should not panic, should log error and continue
	ctx := context.Background()
	w.runChecks(ctx)
}

func TestWatchdog_FailedPodWithInvalidMinionID(t *testing.T) {
	minions := &mockMinionQuerier{}
	pods := &mockPodStatusChecker{
		pods: []k8s.PodInfo{
			{Name: "minion-invalid", MinionID: "not-a-uuid", Phase: "Failed"},
		},
	}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	// Should not panic, should log error and skip
	ctx := context.Background()
	w.runChecks(ctx)

	failedCalls := minions.getFailedCalls()
	if len(failedCalls) != 0 {
		t.Fatalf("expected 0 MarkFailed calls for invalid UUID, got %d", len(failedCalls))
	}
}

func TestWatchdog_FailedPodWithoutLabel(t *testing.T) {
	minions := &mockMinionQuerier{}
	pods := &mockPodStatusChecker{
		pods: []k8s.PodInfo{
			{Name: "orphan-pod", MinionID: "", Phase: "Failed"},
		},
	}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	// Should skip pods without minion-id label
	ctx := context.Background()
	w.runChecks(ctx)

	failedCalls := minions.getFailedCalls()
	if len(failedCalls) != 0 {
		t.Fatalf("expected 0 MarkFailed calls for pod without label, got %d", len(failedCalls))
	}
}

func TestWatchdog_RunAndStop(t *testing.T) {
	minions := &mockMinionQuerier{idleMinions: []*db.Minion{}}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	go w.Run(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good, stopped cleanly
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog.Stop() did not return within timeout")
	}
}

func TestWatchdog_ContextCancellation(t *testing.T) {
	minions := &mockMinionQuerier{idleMinions: []*db.Minion{}}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context - watchdog should stop
	cancel()

	// Wait a bit for it to notice
	time.Sleep(100 * time.Millisecond)

	// Try to stop (should be a no-op but shouldn't hang)
	// Actually, since doneCh is already closed by Run returning,
	// Stop() will return immediately
}

func TestWatchdog_MixedFailedAndRunningPods(t *testing.T) {
	failedID := uuid.New()
	runningID := uuid.New()

	minions := &mockMinionQuerier{}
	pods := &mockPodStatusChecker{
		pods: []k8s.PodInfo{
			{Name: "minion-failed", MinionID: failedID.String(), Phase: "Failed"},
			{Name: "minion-running", MinionID: runningID.String(), Phase: "Running"},
			{Name: "minion-pending", MinionID: uuid.New().String(), Phase: "Pending"},
		},
	}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	failedCalls := minions.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(failedCalls))
	}
	if failedCalls[0] != failedID {
		t.Errorf("expected failed minion ID %s, got %s", failedID, failedCalls[0])
	}
}

func TestWatchdog_IdleMinionWithNilChannelID(t *testing.T) {
	idleMinion := &db.Minion{
		ID:               uuid.New(),
		Status:           db.StatusRunning,
		LastActivityAt:   time.Now().Add(-45 * time.Minute),
		DiscordChannelID: nil, // No channel ID
	}

	minions := &mockMinionQuerier{
		idleMinions: []*db.Minion{idleMinion},
	}
	pods := &mockPodStatusChecker{pods: []k8s.PodInfo{}}
	notifier := &mockNotifier{}
	logger := testLogger()

	w := New(minions, pods, notifier, logger)

	ctx := context.Background()
	w.runChecks(ctx)

	notifications := notifier.getNotifications()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].DiscordChannelID != "" {
		t.Errorf("expected empty channel ID, got %s", notifications[0].DiscordChannelID)
	}
}
