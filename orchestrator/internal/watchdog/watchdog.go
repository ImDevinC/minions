// Package watchdog provides background monitoring for minion health.
// It detects idle minions and failed pods, alerting via Discord webhook.
package watchdog

import (
	"context"
	"log/slog"
	"time"

	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
	"github.com/google/uuid"
)

// Configuration constants for watchdog behavior.
const (
	// CheckInterval is how often the watchdog runs its checks.
	CheckInterval = 5 * time.Minute

	// IdleThreshold is how long a minion can go without activity before being flagged.
	IdleThreshold = 30 * time.Minute
)

// MinionQuerier provides read access to minion data for watchdog checks.
type MinionQuerier interface {
	// ListIdleRunning returns running minions with last_activity_at older than threshold.
	ListIdleRunning(ctx context.Context, idleThreshold time.Duration) ([]*db.Minion, error)

	// MarkFailed marks a minion as failed with the given error message.
	MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error
}

// PodStatusChecker checks pod health status.
type PodStatusChecker interface {
	// ListPods returns all minion pods in the namespace.
	ListPods(ctx context.Context) ([]k8s.PodInfo, error)
}

// Watchdog monitors minion health and alerts on issues.
type Watchdog struct {
	minions  MinionQuerier
	pods     PodStatusChecker
	notifier webhook.Notifier
	logger   *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// New creates a new Watchdog instance.
func New(minions MinionQuerier, pods PodStatusChecker, notifier webhook.Notifier, logger *slog.Logger) *Watchdog {
	return &Watchdog{
		minions:  minions,
		pods:     pods,
		notifier: notifier,
		logger:   logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Run starts the watchdog background loop.
// It runs until Stop is called or the context is cancelled.
func (w *Watchdog) Run(ctx context.Context) {
	defer close(w.doneCh)

	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	w.logger.Info("watchdog started", "check_interval", CheckInterval, "idle_threshold", IdleThreshold)

	// Run an initial check immediately
	w.runChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("watchdog stopping due to context cancellation")
			return
		case <-w.stopCh:
			w.logger.Info("watchdog stopping due to stop signal")
			return
		case <-ticker.C:
			w.runChecks(ctx)
		}
	}
}

// Stop signals the watchdog to stop and waits for it to finish.
func (w *Watchdog) Stop() {
	close(w.stopCh)
	<-w.doneCh
}

// runChecks performs all watchdog checks.
func (w *Watchdog) runChecks(ctx context.Context) {
	w.logger.Debug("running watchdog checks")

	// Check for idle minions
	idleCount := w.checkIdleMinions(ctx)

	// Check for failed pods
	failedCount := w.checkFailedPods(ctx)

	if idleCount > 0 || failedCount > 0 {
		w.logger.Info("watchdog check completed",
			"idle_minions_alerted", idleCount,
			"failed_pods_handled", failedCount,
		)
	}
}

// checkIdleMinions finds running minions with no recent activity and alerts.
func (w *Watchdog) checkIdleMinions(ctx context.Context) int {
	minions, err := w.minions.ListIdleRunning(ctx, IdleThreshold)
	if err != nil {
		w.logger.Error("failed to query idle minions", "error", err)
		return 0
	}

	alertedCount := 0
	for _, m := range minions {
		channelID := ""
		if m.DiscordChannelID != nil {
			channelID = *m.DiscordChannelID
		}

		err := w.notifier.Notify(ctx, webhook.Notification{
			MinionID:         m.ID,
			Type:             webhook.NotifyIdle,
			DiscordChannelID: channelID,
		})
		if err != nil {
			w.logger.Error("failed to send idle notification",
				"minion_id", m.ID,
				"error", err,
			)
			continue
		}

		w.logger.Warn("idle minion detected",
			"minion_id", m.ID,
			"repo", m.Repo,
			"last_activity", m.LastActivityAt,
			"idle_duration", time.Since(m.LastActivityAt).Round(time.Minute),
		)
		alertedCount++
	}

	return alertedCount
}

// checkFailedPods finds pods in Failed phase and marks their minions as failed.
func (w *Watchdog) checkFailedPods(ctx context.Context) int {
	pods, err := w.pods.ListPods(ctx)
	if err != nil {
		w.logger.Error("failed to list pods", "error", err)
		return 0
	}

	handledCount := 0
	for _, pod := range pods {
		// Only handle Failed pods (OOMKilled, Evicted, etc.)
		if pod.Phase != "Failed" {
			continue
		}

		// Skip pods without minion-id label (shouldn't happen, but be defensive)
		if pod.MinionID == "" {
			w.logger.Warn("found failed pod without minion-id label", "pod_name", pod.Name)
			continue
		}

		minionID, err := uuid.Parse(pod.MinionID)
		if err != nil {
			w.logger.Error("failed to parse minion ID from pod label",
				"pod_name", pod.Name,
				"minion_id_raw", pod.MinionID,
				"error", err,
			)
			continue
		}

		// Mark the minion as failed
		errorMsg := "Pod terminated: " + pod.Phase
		if err := w.minions.MarkFailed(ctx, minionID, errorMsg); err != nil {
			w.logger.Error("failed to mark minion as failed",
				"minion_id", minionID,
				"pod_name", pod.Name,
				"error", err,
			)
			continue
		}

		w.logger.Warn("marked minion as failed due to pod failure",
			"minion_id", minionID,
			"pod_name", pod.Name,
			"pod_phase", pod.Phase,
		)
		handledCount++
	}

	return handledCount
}
