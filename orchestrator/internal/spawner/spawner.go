// Package spawner handles background spawning of pods for pending minions.
package spawner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
)

// PollInterval is how often the spawner checks for pending minions.
const PollInterval = 5 * time.Second

// MinionQuerier provides access to pending minion data.
type MinionQuerier interface {
	// ListPending returns minions in pending status ordered by created_at ASC (FIFO).
	ListPending(ctx context.Context) ([]*db.Minion, error)
}

// MinionUpdater provides methods to update minion status.
type MinionUpdater interface {
	// MarkFailed marks a minion as failed with the given error message.
	MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error

	// MarkRunning transitions a minion to running status with its pod name.
	// Must be called after the pod is ready.
	MarkRunning(ctx context.Context, id uuid.UUID, podName string) error
}

// TokenManager generates GitHub App installation tokens for repository access.
type TokenManager interface {
	// GetToken returns an installation token for the given repository.
	// The repo format is "owner/name" (e.g., "anomalyco/minions").
	GetToken(ctx context.Context, repo string) (string, error)
}

// PodSpawner handles pod creation and readiness checks.
type PodSpawner interface {
	// SpawnPodWithRetry creates a pod with exponential backoff retry.
	SpawnPodWithRetry(ctx context.Context, params k8s.SpawnParams) (podName string, err error)

	// WaitForPodReady waits for a pod to become ready.
	WaitForPodReady(ctx context.Context, podName string) error
}

// SSEConnector starts SSE event streaming from a pod.
type SSEConnector interface {
	// Connect starts streaming events from a pod. Runs in a goroutine and
	// reconnects automatically on disconnection. Non-blocking.
	Connect(ctx context.Context, minionID uuid.UUID, podName string)
}

// Config holds configuration for the spawner.
type Config struct {
	// OrchestratorURL is the base URL for orchestrator callbacks.
	OrchestratorURL string

	// InternalAPIToken is used to authenticate with the orchestrator.
	InternalAPIToken string
}

// Spawner polls for pending minions and spawns pods for them.
type Spawner struct {
	minions      MinionQuerier
	minionUpdate MinionUpdater
	tokens       TokenManager
	pods         PodSpawner
	sse          SSEConnector
	config       Config
	logger       *slog.Logger
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// New creates a new Spawner instance.
func New(minions MinionQuerier, minionUpdate MinionUpdater, tokens TokenManager, pods PodSpawner, sse SSEConnector, config Config, logger *slog.Logger) *Spawner {
	return &Spawner{
		minions:      minions,
		minionUpdate: minionUpdate,
		tokens:       tokens,
		pods:         pods,
		sse:          sse,
		config:       config,
		logger:       logger,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start launches the background goroutine that polls for pending minions.
func (s *Spawner) Start(ctx context.Context) {
	go s.run(ctx)
}

// run is the main background loop.
func (s *Spawner) run(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	s.logger.Info("spawner started", "poll_interval", PollInterval)

	// Run an initial poll immediately
	s.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("spawner stopping due to context cancellation")
			return
		case <-s.stopCh:
			s.logger.Info("spawner stopping due to stop signal")
			return
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

// Stop signals the spawner to stop and waits for completion.
func (s *Spawner) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

// poll fetches pending minions and processes them.
func (s *Spawner) poll(ctx context.Context) {
	minions, err := s.minions.ListPending(ctx)
	if err != nil {
		s.logger.Error("failed to list pending minions", "error", err)
		return
	}

	if len(minions) == 0 {
		return
	}

	s.logger.Debug("found pending minions", "count", len(minions))

	// Process minions serially (PRD non-goal: no concurrent spawning)
	for _, m := range minions {
		s.processMinion(ctx, m)
	}
}

// processMinion handles spawning a single minion's pod.
func (s *Spawner) processMinion(ctx context.Context, m *db.Minion) {
	s.logger.Info("processing pending minion",
		"minion_id", m.ID,
		"repo", m.Repo,
		"created_at", m.CreatedAt,
	)

	// spawner-2: Generate GitHub token
	token, err := s.tokens.GetToken(ctx, m.Repo)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate GitHub token: %v", err)
		s.logger.Error("github token generation failed",
			"minion_id", m.ID,
			"repo", m.Repo,
			"error", err,
		)
		if markErr := s.minionUpdate.MarkFailed(ctx, m.ID, errMsg); markErr != nil {
			s.logger.Error("failed to mark minion as failed",
				"minion_id", m.ID,
				"error", markErr,
			)
		}
		return
	}

	s.logger.Debug("generated github token",
		"minion_id", m.ID,
		"repo", m.Repo,
	)

	// spawner-3: Spawn pod with retry
	params := k8s.SpawnParams{
		MinionID:         m.ID,
		Repo:             m.Repo,
		Task:             m.Task,
		Model:            m.Model,
		GitHubToken:      token,
		OrchestratorURL:  s.config.OrchestratorURL,
		InternalAPIToken: s.config.InternalAPIToken,
	}

	podName, err := s.pods.SpawnPodWithRetry(ctx, params)
	if err != nil {
		errMsg := fmt.Sprintf("failed to spawn pod after retries: %v", err)
		s.logger.Error("pod spawn failed",
			"minion_id", m.ID,
			"repo", m.Repo,
			"error", err,
		)
		if markErr := s.minionUpdate.MarkFailed(ctx, m.ID, errMsg); markErr != nil {
			s.logger.Error("failed to mark minion as failed",
				"minion_id", m.ID,
				"error", markErr,
			)
		}
		return
	}

	s.logger.Info("pod spawned, waiting for readiness",
		"minion_id", m.ID,
		"pod_name", podName,
	)

	// spawner-3: Wait for pod to become ready
	if err := s.pods.WaitForPodReady(ctx, podName); err != nil {
		errMsg := fmt.Sprintf("pod failed to become ready: %v", err)
		s.logger.Error("pod readiness wait failed",
			"minion_id", m.ID,
			"pod_name", podName,
			"error", err,
		)
		if markErr := s.minionUpdate.MarkFailed(ctx, m.ID, errMsg); markErr != nil {
			s.logger.Error("failed to mark minion as failed",
				"minion_id", m.ID,
				"error", markErr,
			)
		}
		return
	}

	s.logger.Info("pod is ready",
		"minion_id", m.ID,
		"pod_name", podName,
	)

	// spawner-4: Mark minion as running with pod name
	if err := s.minionUpdate.MarkRunning(ctx, m.ID, podName); err != nil {
		// Log but don't crash - the pod is running and we don't want to
		// orphan it. The reconciler can recover this state if needed.
		s.logger.Error("failed to mark minion as running",
			"minion_id", m.ID,
			"pod_name", podName,
			"error", err,
		)
		return
	}

	s.logger.Info("minion marked as running",
		"minion_id", m.ID,
		"pod_name", podName,
	)

	// spawner-5: Initiate SSE streaming
	// Connection failures are non-fatal; SSEClient retries automatically.
	s.sse.Connect(ctx, m.ID, podName)
	s.logger.Info("SSE streaming initiated",
		"minion_id", m.ID,
		"pod_name", podName,
	)
}
