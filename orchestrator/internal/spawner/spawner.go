// Package spawner handles background spawning of pods for pending minions.
package spawner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/anomalyco/minions/orchestrator/internal/db"
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
}

// TokenManager generates GitHub App installation tokens for repository access.
type TokenManager interface {
	// GetToken returns an installation token for the given repository.
	// The repo format is "owner/name" (e.g., "anomalyco/minions").
	GetToken(ctx context.Context, repo string) (string, error)
}

// Spawner polls for pending minions and spawns pods for them.
type Spawner struct {
	minions      MinionQuerier
	minionUpdate MinionUpdater
	tokens       TokenManager
	logger       *slog.Logger
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// New creates a new Spawner instance.
func New(minions MinionQuerier, minionUpdate MinionUpdater, tokens TokenManager, logger *slog.Logger) *Spawner {
	return &Spawner{
		minions:      minions,
		minionUpdate: minionUpdate,
		tokens:       tokens,
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

	// Token will be passed to SpawnParams in spawner-3
	_ = token

	// TODO(spawner-3 through spawner-6): implement full spawn logic
	// - spawner-3: SpawnPodWithRetry + WaitForPodReady
	// - spawner-4: MarkRunning
	// - spawner-5: Initiate SSE streaming
	// - spawner-6: Error handling
}
