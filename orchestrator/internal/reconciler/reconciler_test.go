package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/imdevinc/minions/orchestrator/internal/db"
	"github.com/imdevinc/minions/orchestrator/internal/k8s"
)

// mockMinionStore is a test double for MinionStore.
type mockMinionStore struct {
	minions      []*db.Minion
	markedFailed map[uuid.UUID]string
	listErr      error
	markErr      error
}

func (m *mockMinionStore) ListByStatuses(ctx context.Context, statuses []db.MinionStatus) ([]*db.Minion, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Filter by statuses
	statusSet := make(map[db.MinionStatus]bool)
	for _, s := range statuses {
		statusSet[s] = true
	}
	var result []*db.Minion
	for _, minion := range m.minions {
		if statusSet[minion.Status] {
			result = append(result, minion)
		}
	}
	return result, nil
}

func (m *mockMinionStore) MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error {
	if m.markErr != nil {
		return m.markErr
	}
	if m.markedFailed == nil {
		m.markedFailed = make(map[uuid.UUID]string)
	}
	m.markedFailed[id] = errorMsg
	return nil
}

// mockPodManager is a test double for PodManager.
type mockPodManager struct {
	pods       []k8s.PodInfo
	terminated []string
	listErr    error
	termErr    error
}

func (m *mockPodManager) ListPods(ctx context.Context) ([]k8s.PodInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.pods, nil
}

func (m *mockPodManager) TerminatePod(ctx context.Context, podName string) error {
	if m.termErr != nil {
		return m.termErr
	}
	m.terminated = append(m.terminated, podName)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestReconciler_Run_NoMinionsNoPods(t *testing.T) {
	store := &mockMinionStore{}
	pods := &mockPodManager{}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OrphanedMinions != 0 {
		t.Errorf("expected 0 orphaned minions, got %d", result.OrphanedMinions)
	}
	if result.StrayPods != 0 {
		t.Errorf("expected 0 stray pods, got %d", result.StrayPods)
	}
}

func TestReconciler_Run_OrphanedMinion_NoPod(t *testing.T) {
	minionID := uuid.New()
	store := &mockMinionStore{
		minions: []*db.Minion{
			{ID: minionID, Status: db.StatusRunning},
		},
	}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{}, // no pods
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OrphanedMinions != 1 {
		t.Errorf("expected 1 orphaned minion, got %d", result.OrphanedMinions)
	}
	if len(store.markedFailed) != 1 {
		t.Errorf("expected 1 minion marked failed, got %d", len(store.markedFailed))
	}
	if _, ok := store.markedFailed[minionID]; !ok {
		t.Errorf("expected minion %s to be marked failed", minionID)
	}
}

func TestReconciler_Run_OrphanedMinion_TerminalPod(t *testing.T) {
	minionID := uuid.New()
	podName := "minion-" + minionID.String()

	store := &mockMinionStore{
		minions: []*db.Minion{
			{ID: minionID, Status: db.StatusRunning},
		},
	}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{
			{Name: podName, MinionID: minionID.String(), Phase: "Failed"},
		},
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OrphanedMinions != 1 {
		t.Errorf("expected 1 orphaned minion, got %d", result.OrphanedMinions)
	}
	errMsg := store.markedFailed[minionID]
	if errMsg != "pod in terminal phase: Failed" {
		t.Errorf("expected terminal phase error, got: %s", errMsg)
	}
}

func TestReconciler_Run_StrayPod_NoMinion(t *testing.T) {
	strayMinionID := uuid.New().String()
	podName := "minion-" + strayMinionID

	store := &mockMinionStore{
		minions: []*db.Minion{}, // no minions
	}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{
			{Name: podName, MinionID: strayMinionID, Phase: "Running"},
		},
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StrayPods != 1 {
		t.Errorf("expected 1 stray pod, got %d", result.StrayPods)
	}
	if len(pods.terminated) != 1 || pods.terminated[0] != podName {
		t.Errorf("expected pod %s to be terminated, got %v", podName, pods.terminated)
	}
}

func TestReconciler_Run_StrayPod_NoLabel(t *testing.T) {
	store := &mockMinionStore{}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{
			{Name: "minion-unknown", MinionID: "", Phase: "Running"},
		},
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StrayPods != 1 {
		t.Errorf("expected 1 stray pod, got %d", result.StrayPods)
	}
}

func TestReconciler_Run_HealthyMinion_NotOrphaned(t *testing.T) {
	minionID := uuid.New()
	podName := "minion-" + minionID.String()

	store := &mockMinionStore{
		minions: []*db.Minion{
			{ID: minionID, Status: db.StatusRunning},
		},
	}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{
			{Name: podName, MinionID: minionID.String(), Phase: "Running"},
		},
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OrphanedMinions != 0 {
		t.Errorf("expected 0 orphaned minions, got %d", result.OrphanedMinions)
	}
	if result.StrayPods != 0 {
		t.Errorf("expected 0 stray pods, got %d", result.StrayPods)
	}
	if len(store.markedFailed) != 0 {
		t.Errorf("expected no minions marked failed")
	}
	if len(pods.terminated) != 0 {
		t.Errorf("expected no pods terminated")
	}
}

func TestReconciler_Run_PendingMinion_Orphaned(t *testing.T) {
	minionID := uuid.New()

	store := &mockMinionStore{
		minions: []*db.Minion{
			{ID: minionID, Status: db.StatusPending}, // pending, no pod yet
		},
	}
	pods := &mockPodManager{
		pods: []k8s.PodInfo{}, // no pods
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Pending minions without pods ARE orphaned after restart
	// (they should have a pod being created or have failed)
	if result.OrphanedMinions != 1 {
		t.Errorf("expected 1 orphaned minion, got %d", result.OrphanedMinions)
	}
}

func TestReconciler_Run_ListMinionsError(t *testing.T) {
	store := &mockMinionStore{
		listErr: errors.New("db connection failed"),
	}
	pods := &mockPodManager{}

	rec := New(store, pods, testLogger())
	_, err := rec.Run(context.Background())

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, store.listErr) {
		t.Errorf("expected db error, got: %v", err)
	}
}

func TestReconciler_Run_ListPodsError(t *testing.T) {
	store := &mockMinionStore{
		minions: []*db.Minion{
			{ID: uuid.New(), Status: db.StatusRunning},
		},
	}
	pods := &mockPodManager{
		listErr: errors.New("k8s API unavailable"),
	}

	rec := New(store, pods, testLogger())
	_, err := rec.Run(context.Background())

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestReconciler_Run_MarkFailedError_ContinuesOthers(t *testing.T) {
	minionID1 := uuid.New()
	minionID2 := uuid.New()

	// Store that fails on first mark but succeeds on second
	store := &failOnceMinionStore{
		minions: []*db.Minion{
			{ID: minionID1, Status: db.StatusRunning},
			{ID: minionID2, Status: db.StatusRunning},
		},
		markedFailed: make(map[uuid.UUID]string),
	}

	pods := &mockPodManager{
		pods: []k8s.PodInfo{}, // no pods, both are orphaned
	}

	rec := New(store, pods, testLogger())
	result, err := rec.Run(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have attempted both, succeeded on one
	if result.OrphanedMinions != 1 {
		t.Errorf("expected 1 orphaned minion marked, got %d", result.OrphanedMinions)
	}
}

// failOnceMinionStore fails the first MarkFailed call but succeeds on subsequent calls.
type failOnceMinionStore struct {
	minions       []*db.Minion
	markedFailed  map[uuid.UUID]string
	markCallCount int
}

func (m *failOnceMinionStore) ListByStatuses(ctx context.Context, statuses []db.MinionStatus) ([]*db.Minion, error) {
	statusSet := make(map[db.MinionStatus]bool)
	for _, s := range statuses {
		statusSet[s] = true
	}
	var result []*db.Minion
	for _, minion := range m.minions {
		if statusSet[minion.Status] {
			result = append(result, minion)
		}
	}
	return result, nil
}

func (m *failOnceMinionStore) MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error {
	m.markCallCount++
	if m.markCallCount == 1 {
		return errors.New("db error")
	}
	m.markedFailed[id] = errorMsg
	return nil
}
