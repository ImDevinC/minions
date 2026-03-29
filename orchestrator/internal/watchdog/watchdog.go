// Package watchdog provides background monitoring for minion health.
// It detects idle minions and failed pods, alerting via Discord webhook.
package watchdog

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/imdevinc/minions/orchestrator/internal/db"
	"github.com/imdevinc/minions/orchestrator/internal/k8s"
	"github.com/imdevinc/minions/orchestrator/internal/webhook"
)

// Configuration constants for watchdog behavior.
const (
	// CheckInterval is how often the watchdog runs its checks.
	CheckInterval = 5 * time.Minute

	// IdleThreshold is how long a minion can go without activity before being flagged.
	IdleThreshold = 30 * time.Minute

	// ClarificationTimeout is how long a minion can wait for clarification before timing out.
	ClarificationTimeout = 24 * time.Hour

	// ClarificationCheckInterval is how often to check for clarification timeouts.
	// The PRD says every 10 minutes, but we piggyback on the existing CheckInterval (5 min).
	// To maintain the 10-min cadence, we track state and skip every other check.
	ClarificationCheckInterval = 10 * time.Minute

	// TerminalPodCleanupDelay is how long to keep completed/failed/terminated minion pods
	// before the orchestrator performs cleanup.
	TerminalPodCleanupDelay = 1 * time.Hour
)

// MinionQuerier provides read access to minion data for watchdog checks.
type MinionQuerier interface {
	// ListIdleRunning returns running minions with last_activity_at older than threshold.
	ListIdleRunning(ctx context.Context, idleThreshold time.Duration) ([]*db.Minion, error)

	// ListClarificationTimeouts returns minions in awaiting_clarification with created_at older than timeout.
	ListClarificationTimeouts(ctx context.Context, timeout time.Duration) ([]*db.Minion, error)

	// MarkFailed marks a minion as failed with the given error message.
	MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error

	// ListTerminalWithPodOlderThan returns terminal minions that still have pod_name set
	// and were completed before the given age threshold.
	ListTerminalWithPodOlderThan(ctx context.Context, age time.Duration) ([]*db.Minion, error)

	// ClearPodName clears pod_name for a minion after pod cleanup.
	ClearPodName(ctx context.Context, id uuid.UUID) error
}

// PodStatusChecker checks pod health status.
type PodStatusChecker interface {
	// ListPods returns all minion pods in the namespace.
	ListPods(ctx context.Context) ([]k8s.PodInfo, error)

	// TerminatePod deletes a pod by name.
	TerminatePod(ctx context.Context, podName string) error
}

// SSEDisconnector handles disconnecting SSE streams from minions.
type SSEDisconnector interface {
	// Disconnect stops streaming events for a minion.
	Disconnect(minionID uuid.UUID)
}

// Watchdog monitors minion health and alerts on issues.
type Watchdog struct {
	minions              MinionQuerier
	pods                 PodStatusChecker
	sse                  SSEDisconnector
	notifier             webhook.Notifier
	logger               *slog.Logger
	stopCh               chan struct{}
	doneCh               chan struct{}
	lastClarificationChk time.Time
}

// New creates a new Watchdog instance.
func New(minions MinionQuerier, pods PodStatusChecker, sse SSEDisconnector, notifier webhook.Notifier, logger *slog.Logger) *Watchdog {
	return &Watchdog{
		minions:  minions,
		pods:     pods,
		sse:      sse,
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

	// Check for clarification timeouts (every 10 minutes)
	clarificationTimeoutCount := 0
	if time.Since(w.lastClarificationChk) >= ClarificationCheckInterval {
		clarificationTimeoutCount = w.checkClarificationTimeouts(ctx)
		w.lastClarificationChk = time.Now()
	}

	// Clean up terminal minion pods that have exceeded retention period.
	terminalCleanupCount := w.cleanupTerminalPods(ctx)

	if idleCount > 0 || failedCount > 0 || clarificationTimeoutCount > 0 || terminalCleanupCount > 0 {
		w.logger.Info("watchdog check completed",
			"idle_minions_alerted", idleCount,
			"failed_pods_handled", failedCount,
			"clarification_timeouts", clarificationTimeoutCount,
			"terminal_pods_cleaned", terminalCleanupCount,
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

		// Clean up SSE connection
		w.sse.Disconnect(minionID)

		w.logger.Warn("marked minion as failed due to pod failure",
			"minion_id", minionID,
			"pod_name", pod.Name,
			"pod_phase", pod.Phase,
		)
		handledCount++
	}

	return handledCount
}

// checkClarificationTimeouts finds minions stuck in awaiting_clarification for too long.
// Marks them as failed and notifies Discord.
func (w *Watchdog) checkClarificationTimeouts(ctx context.Context) int {
	minions, err := w.minions.ListClarificationTimeouts(ctx, ClarificationTimeout)
	if err != nil {
		w.logger.Error("failed to query clarification timeouts", "error", err)
		return 0
	}

	handledCount := 0
	for _, m := range minions {
		// Mark as failed
		if err := w.minions.MarkFailed(ctx, m.ID, "Clarification timeout"); err != nil {
			w.logger.Error("failed to mark timed-out minion as failed",
				"minion_id", m.ID,
				"error", err,
			)
			continue
		}

		// Clean up SSE connection
		w.sse.Disconnect(m.ID)

		// Notify Discord
		channelID := ""
		if m.DiscordChannelID != nil {
			channelID = *m.DiscordChannelID
		}

		err := w.notifier.Notify(ctx, webhook.Notification{
			MinionID:         m.ID,
			Type:             webhook.NotifyClarificationTimeout,
			DiscordChannelID: channelID,
		})
		if err != nil {
			w.logger.Error("failed to send clarification timeout notification",
				"minion_id", m.ID,
				"error", err,
			)
			// Continue anyway, minion is already marked failed
		}

		w.logger.Warn("minion timed out waiting for clarification",
			"minion_id", m.ID,
			"repo", m.Repo,
			"created_at", m.CreatedAt,
			"timeout_duration", ClarificationTimeout,
		)
		handledCount++
	}

	return handledCount
}

// cleanupTerminalPods deletes pods for terminal minions once retention has elapsed.
func (w *Watchdog) cleanupTerminalPods(ctx context.Context) int {
	minions, err := w.minions.ListTerminalWithPodOlderThan(ctx, TerminalPodCleanupDelay)
	if err != nil {
		w.logger.Error("failed to query terminal minions for pod cleanup", "error", err)
		return 0
	}

	cleanedCount := 0
	for _, m := range minions {
		if m.PodName == nil || *m.PodName == "" {
			continue
		}

		if err := w.pods.TerminatePod(ctx, *m.PodName); err != nil {
			w.logger.Error("failed to terminate terminal minion pod",
				"minion_id", m.ID,
				"pod_name", *m.PodName,
				"error", err,
			)
			continue
		}

		if err := w.minions.ClearPodName(ctx, m.ID); err != nil {
			w.logger.Error("failed to clear pod_name after pod cleanup",
				"minion_id", m.ID,
				"pod_name", *m.PodName,
				"error", err,
			)
			continue
		}

		w.logger.Info("cleaned up terminal minion pod",
			"minion_id", m.ID,
			"pod_name", *m.PodName,
		)
		cleanedCount++
	}

	return cleanedCount
}
