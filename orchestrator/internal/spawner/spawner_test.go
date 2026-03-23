package spawner

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
)

// --- Test Mocks ---

type mockMinionQuerier struct {
	minions []*db.Minion
	err     error
}

func (m *mockMinionQuerier) ListPending(ctx context.Context) ([]*db.Minion, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.minions, nil
}

type mockMinionUpdater struct {
	failedCalls       map[uuid.UUID]string
	runningCalls      map[uuid.UUID]string
	passwordCalls     map[uuid.UUID]string
	markFailedErr     error
	markRunningErr    error
	storePasswordErr  error
	mu                sync.Mutex
	markRunningCalled bool
}

func (m *mockMinionUpdater) MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failedCalls == nil {
		m.failedCalls = make(map[uuid.UUID]string)
	}
	m.failedCalls[id] = errorMsg
	return m.markFailedErr
}

func (m *mockMinionUpdater) MarkRunning(ctx context.Context, id uuid.UUID, podName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markRunningCalled = true
	if m.runningCalls == nil {
		m.runningCalls = make(map[uuid.UUID]string)
	}
	m.runningCalls[id] = podName
	return m.markRunningErr
}

func (m *mockMinionUpdater) StorePassword(ctx context.Context, id uuid.UUID, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.passwordCalls == nil {
		m.passwordCalls = make(map[uuid.UUID]string)
	}
	m.passwordCalls[id] = password
	return m.storePasswordErr
}

func (m *mockMinionUpdater) getFailedCalls() map[uuid.UUID]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[uuid.UUID]string)
	for k, v := range m.failedCalls {
		result[k] = v
	}
	return result
}

func (m *mockMinionUpdater) getRunningCalls() map[uuid.UUID]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[uuid.UUID]string)
	for k, v := range m.runningCalls {
		result[k] = v
	}
	return result
}

func (m *mockMinionUpdater) wasMarkRunningCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.markRunningCalled
}

type mockTokenManager struct {
	tokens map[string]string
	err    error
}

func (m *mockTokenManager) GetToken(ctx context.Context, repo string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.tokens == nil {
		return "test-token", nil
	}
	return m.tokens[repo], nil
}

type mockPodSpawner struct {
	podName           string
	spawnErr          error
	readyErr          error
	spawnCalls        []k8s.SpawnParams
	waitForReadyCalls []string
	mu                sync.Mutex
}

func (m *mockPodSpawner) SpawnPodWithRetry(ctx context.Context, params k8s.SpawnParams) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnCalls = append(m.spawnCalls, params)
	if m.spawnErr != nil {
		return "", m.spawnErr
	}
	if m.podName == "" {
		return "minion-" + params.MinionID.String(), nil
	}
	return m.podName, nil
}

func (m *mockPodSpawner) WaitForPodReady(ctx context.Context, podName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitForReadyCalls = append(m.waitForReadyCalls, podName)
	return m.readyErr
}

func (m *mockPodSpawner) getSpawnCalls() []k8s.SpawnParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]k8s.SpawnParams{}, m.spawnCalls...)
}

func (m *mockPodSpawner) getWaitForReadyCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.waitForReadyCalls...)
}

type mockSSEConnector struct {
	connectCalls []struct {
		minionID uuid.UUID
		podName  string
		password string
	}
	mu sync.Mutex
}

func (m *mockSSEConnector) Connect(ctx context.Context, minionID uuid.UUID, podName string, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCalls = append(m.connectCalls, struct {
		minionID uuid.UUID
		podName  string
		password string
	}{minionID, podName, password})
}

func (m *mockSSEConnector) Disconnect(minionID uuid.UUID) {
	// No-op for tests
}

func (m *mockSSEConnector) getConnectCalls() []struct {
	minionID uuid.UUID
	podName  string
	password string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]struct {
		minionID uuid.UUID
		podName  string
		password string
	}{}, m.connectCalls...)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func testConfig() Config {
	return Config{
		OrchestratorURL:  "http://orchestrator:8080",
		InternalAPIToken: "test-api-token",
	}
}

// --- Tests ---

func TestSpawner_ProcessesPendingMinions(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:        minionID,
		Repo:      "owner/repo",
		Task:      "fix the bug",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	// Run a single poll
	ctx := context.Background()
	s.poll(ctx)

	// Verify pod was spawned with correct params
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawnCalls))
	}
	params := spawnCalls[0]
	if params.MinionID != minionID {
		t.Errorf("expected minion ID %s, got %s", minionID, params.MinionID)
	}
	if params.Repo != "owner/repo" {
		t.Errorf("expected repo owner/repo, got %s", params.Repo)
	}
	if params.Task != "fix the bug" {
		t.Errorf("expected task 'fix the bug', got %s", params.Task)
	}
	if params.GitHubToken != "test-token" {
		t.Errorf("expected token 'test-token', got %s", params.GitHubToken)
	}
	if params.OrchestratorURL != "http://orchestrator:8080" {
		t.Errorf("expected orchestrator URL, got %s", params.OrchestratorURL)
	}

	// Verify minion was marked running
	runningCalls := updater.getRunningCalls()
	if len(runningCalls) != 1 {
		t.Fatalf("expected 1 running call, got %d", len(runningCalls))
	}
	expectedPodName := "minion-" + minionID.String()
	if runningCalls[minionID] != expectedPodName {
		t.Errorf("expected pod name %s, got %s", expectedPodName, runningCalls[minionID])
	}

	// Verify SSE was initiated
	connectCalls := sse.getConnectCalls()
	if len(connectCalls) != 1 {
		t.Fatalf("expected 1 SSE connect call, got %d", len(connectCalls))
	}
	if connectCalls[0].minionID != minionID {
		t.Errorf("expected minion ID %s in SSE call, got %s", minionID, connectCalls[0].minionID)
	}
}

func TestSpawner_SkipsNonPendingMinions(t *testing.T) {
	// ListPending only returns pending minions, so if we return nothing,
	// nothing should happen. This tests that the spawner respects what
	// ListPending returns (i.e., only pending minions).
	querier := &mockMinionQuerier{minions: []*db.Minion{}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// No spawn calls should have been made
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 0 {
		t.Errorf("expected 0 spawn calls, got %d", len(spawnCalls))
	}

	// No running calls
	runningCalls := updater.getRunningCalls()
	if len(runningCalls) != 0 {
		t.Errorf("expected 0 running calls, got %d", len(runningCalls))
	}
}

func TestSpawner_HandlesTokenGenerationFailure(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:        minionID,
		Repo:      "owner/repo",
		Task:      "fix it",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{err: errors.New("GitHub App not installed")}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// No spawn calls should have been made
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 0 {
		t.Errorf("expected 0 spawn calls after token failure, got %d", len(spawnCalls))
	}

	// Minion should be marked failed
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 failed call, got %d", len(failedCalls))
	}
	errMsg := failedCalls[minionID]
	if errMsg == "" {
		t.Error("expected error message in MarkFailed call")
	}
	if !contains(errMsg, "GitHub token") {
		t.Errorf("expected error message to mention GitHub token, got: %s", errMsg)
	}

	// No SSE connection should be initiated
	connectCalls := sse.getConnectCalls()
	if len(connectCalls) != 0 {
		t.Errorf("expected 0 SSE connect calls, got %d", len(connectCalls))
	}
}

func TestSpawner_HandlesPodSpawnFailure(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:        minionID,
		Repo:      "owner/repo",
		Task:      "fix it",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{spawnErr: errors.New("k8s API rate limited")}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Spawn was attempted
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Errorf("expected 1 spawn call, got %d", len(spawnCalls))
	}

	// No wait for ready calls (spawn failed)
	waitCalls := pods.getWaitForReadyCalls()
	if len(waitCalls) != 0 {
		t.Errorf("expected 0 wait calls after spawn failure, got %d", len(waitCalls))
	}

	// Minion should be marked failed
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 failed call, got %d", len(failedCalls))
	}
	errMsg := failedCalls[minionID]
	if !contains(errMsg, "spawn pod") {
		t.Errorf("expected error message to mention pod spawn, got: %s", errMsg)
	}

	// MarkRunning should NOT be called
	if updater.wasMarkRunningCalled() {
		t.Error("MarkRunning should not be called after spawn failure")
	}
}

func TestSpawner_HandlesPodReadinessFailure(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:        minionID,
		Repo:      "owner/repo",
		Task:      "fix it",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{readyErr: errors.New("pod readiness timeout")}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Spawn was successful
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Errorf("expected 1 spawn call, got %d", len(spawnCalls))
	}

	// Wait for ready was called
	waitCalls := pods.getWaitForReadyCalls()
	if len(waitCalls) != 1 {
		t.Errorf("expected 1 wait call, got %d", len(waitCalls))
	}

	// Minion should be marked failed
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 failed call, got %d", len(failedCalls))
	}
	errMsg := failedCalls[minionID]
	if !contains(errMsg, "ready") {
		t.Errorf("expected error message to mention readiness, got: %s", errMsg)
	}

	// MarkRunning should NOT be called
	if updater.wasMarkRunningCalled() {
		t.Error("MarkRunning should not be called after readiness failure")
	}
}

func TestSpawner_StopsCleanlyOnShutdownSignal(t *testing.T) {
	querier := &mockMinionQuerier{minions: []*db.Minion{}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good, stopped cleanly
	case <-time.After(1 * time.Second):
		t.Fatal("spawner.Stop() did not return within timeout")
	}
}

func TestSpawner_StopsOnContextCancellation(t *testing.T) {
	querier := &mockMinionQuerier{minions: []*db.Minion{}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context - spawner should stop
	cancel()

	// Wait a bit for it to notice
	time.Sleep(100 * time.Millisecond)

	// Stop should return immediately (doneCh already closed)
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("spawner.Stop() hung after context cancellation")
	}
}

func TestSpawner_ContinuesAfterSingleMinionFailure(t *testing.T) {
	minionID1 := uuid.New()
	minionID2 := uuid.New()
	minion1 := &db.Minion{
		ID:        minionID1,
		Repo:      "owner/repo1",
		Task:      "task 1",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}
	minion2 := &db.Minion{
		ID:        minionID2,
		Repo:      "owner/repo2",
		Task:      "task 2",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now().Add(time.Second),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion1, minion2}}
	updater := &mockMinionUpdater{}
	// Token manager that fails for first repo but succeeds for second
	tokens := &failOnceTokenManager{
		failRepo: "owner/repo1",
	}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// First minion should be marked failed
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 failed call, got %d", len(failedCalls))
	}
	if _, ok := failedCalls[minionID1]; !ok {
		t.Errorf("expected minion1 to be marked failed")
	}

	// Second minion should be processed successfully
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call (for minion2), got %d", len(spawnCalls))
	}
	if spawnCalls[0].MinionID != minionID2 {
		t.Errorf("expected spawn for minion2, got %s", spawnCalls[0].MinionID)
	}

	// Second minion should be marked running
	runningCalls := updater.getRunningCalls()
	if len(runningCalls) != 1 {
		t.Fatalf("expected 1 running call, got %d", len(runningCalls))
	}
	if _, ok := runningCalls[minionID2]; !ok {
		t.Errorf("expected minion2 to be marked running")
	}
}

func TestSpawner_HandlesConcurrentSpawnErrInvalidStatusTransition(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:        minionID,
		Repo:      "owner/repo",
		Task:      "fix it",
		Model:     "claude-3",
		Status:    db.StatusPending,
		CreatedAt: time.Now(),
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	// MarkRunning returns ErrInvalidStatusTransition (simulating concurrent spawn)
	updater := &mockMinionUpdater{markRunningErr: db.ErrInvalidStatusTransition}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Pod was spawned
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Errorf("expected 1 spawn call, got %d", len(spawnCalls))
	}

	// MarkRunning was attempted
	if !updater.wasMarkRunningCalled() {
		t.Error("expected MarkRunning to be called")
	}

	// SSE should NOT be initiated (another spawner won)
	connectCalls := sse.getConnectCalls()
	if len(connectCalls) != 0 {
		t.Errorf("expected 0 SSE connect calls (concurrent spawn), got %d", len(connectCalls))
	}

	// Minion should NOT be marked failed (it was a valid race)
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 0 {
		t.Errorf("expected 0 failed calls (race is not a failure), got %d", len(failedCalls))
	}
}

func TestSpawner_HandlesListPendingError(t *testing.T) {
	querier := &mockMinionQuerier{err: errors.New("database connection lost")}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	// Should not panic, should handle error gracefully
	ctx := context.Background()
	s.poll(ctx)

	// No further actions should be taken
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 0 {
		t.Errorf("expected 0 spawn calls after ListPending error, got %d", len(spawnCalls))
	}
}

func TestSpawner_StoresPasswordBeforeSpawningPod(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:     minionID,
		Status: db.StatusPending,
		Repo:   "owner/repo",
		Task:   "test task",
		Model:  "test-model",
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{tokens: map[string]string{"owner/repo": "test-token"}}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Verify password was stored
	if updater.passwordCalls == nil || len(updater.passwordCalls) != 1 {
		t.Fatalf("expected 1 StorePassword call, got %d", len(updater.passwordCalls))
	}

	storedPassword := updater.passwordCalls[minionID]
	if storedPassword == "" {
		t.Error("expected non-empty password")
	}

	// Verify password is a valid UUID format
	_, err := uuid.Parse(storedPassword)
	if err != nil {
		t.Errorf("expected password to be valid UUID, got %q", storedPassword)
	}

	// Verify pod was spawned with the password
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawnCalls))
	}

	if spawnCalls[0].OpencodePassword != storedPassword {
		t.Errorf("expected pod to be spawned with password %q, got %q", storedPassword, spawnCalls[0].OpencodePassword)
	}

	// Verify SSE was initiated with the password
	sseConnections := sse.getConnectCalls()
	if len(sseConnections) != 1 {
		t.Fatalf("expected 1 SSE connection, got %d", len(sseConnections))
	}

	if sseConnections[0].password != storedPassword {
		t.Errorf("expected SSE connection with password %q, got %q", storedPassword, sseConnections[0].password)
	}
}

func TestSpawner_ReusesExistingPasswordOnRetry(t *testing.T) {
	minionID := uuid.New()
	existingPassword := uuid.New().String()
	minion := &db.Minion{
		ID:               minionID,
		Status:           db.StatusPending,
		Repo:             "owner/repo",
		Task:             "test task",
		Model:            "test-model",
		OpencodePassword: &existingPassword, // Already has a password (crash recovery scenario)
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{}
	tokens := &mockTokenManager{tokens: map[string]string{"owner/repo": "test-token"}}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Verify existing password was reused (idempotent)
	if len(updater.passwordCalls) != 1 {
		t.Fatalf("expected 1 StorePassword call, got %d", len(updater.passwordCalls))
	}

	storedPassword := updater.passwordCalls[minionID]
	if storedPassword != existingPassword {
		t.Errorf("expected to reuse existing password %q, got %q", existingPassword, storedPassword)
	}

	// Verify pod was spawned with existing password
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawnCalls))
	}

	if spawnCalls[0].OpencodePassword != existingPassword {
		t.Errorf("expected pod to be spawned with existing password %q, got %q", existingPassword, spawnCalls[0].OpencodePassword)
	}
}

func TestSpawner_HandlesStorePasswordFailure(t *testing.T) {
	minionID := uuid.New()
	minion := &db.Minion{
		ID:     minionID,
		Status: db.StatusPending,
		Repo:   "owner/repo",
		Task:   "test task",
		Model:  "test-model",
	}

	querier := &mockMinionQuerier{minions: []*db.Minion{minion}}
	updater := &mockMinionUpdater{storePasswordErr: errors.New("database connection lost")}
	tokens := &mockTokenManager{tokens: map[string]string{"owner/repo": "test-token"}}
	pods := &mockPodSpawner{}
	sse := &mockSSEConnector{}

	s := New(querier, updater, tokens, pods, sse, testConfig(), testLogger())

	ctx := context.Background()
	s.poll(ctx)

	// Verify minion was marked as failed
	failedCalls := updater.getFailedCalls()
	if len(failedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(failedCalls))
	}

	errorMsg := failedCalls[minionID]
	if !contains(errorMsg, "failed to store opencode password") {
		t.Errorf("expected error message to contain 'failed to store opencode password', got %q", errorMsg)
	}

	// Verify pod was NOT spawned
	spawnCalls := pods.getSpawnCalls()
	if len(spawnCalls) != 0 {
		t.Errorf("expected 0 spawn calls after password storage failure, got %d", len(spawnCalls))
	}

	// Verify SSE was NOT initiated
	sseConnections := sse.getConnectCalls()
	if len(sseConnections) != 0 {
		t.Errorf("expected 0 SSE connections after password storage failure, got %d", len(sseConnections))
	}
}

// --- Test Helpers ---

// failOnceTokenManager fails for a specific repo.
type failOnceTokenManager struct {
	failRepo string
}

func (m *failOnceTokenManager) GetToken(ctx context.Context, repo string) (string, error) {
	if repo == m.failRepo {
		return "", errors.New("token generation failed for this repo")
	}
	return "test-token-" + repo, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
