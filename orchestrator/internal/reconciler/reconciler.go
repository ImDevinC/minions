// Package reconciler handles startup reconciliation between DB state and k8s pods.
//
// On orchestrator startup, this reconciler:
// 1. Finds all minions in pending/running state
// 2. Checks if their corresponding pods exist and are healthy
// 3. Marks orphaned minions (no pod or terminal pod) as failed
// 4. Deletes stray pods not associated with any known minion
package reconciler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/imdevinc/minions/orchestrator/internal/db"
	"github.com/imdevinc/minions/orchestrator/internal/k8s"
)

// MinionStore is the interface for minion database operations needed by reconciler.
type MinionStore interface {
	ListByStatuses(ctx context.Context, statuses []db.MinionStatus) ([]*db.Minion, error)
	MarkFailed(ctx context.Context, id uuid.UUID, errorMsg string) error
}

// PodManager is the interface for pod operations needed by reconciler.
type PodManager interface {
	k8s.PodLister
	k8s.PodTerminator
}

// Reconciler performs startup reconciliation.
type Reconciler struct {
	minions MinionStore
	pods    PodManager
	logger  *slog.Logger
}

// New creates a new Reconciler.
func New(minions MinionStore, pods PodManager, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		minions: minions,
		pods:    pods,
		logger:  logger,
	}
}

// Result contains the reconciliation results.
type Result struct {
	OrphanedMinions int // minions marked as failed (pod missing or terminal)
	StrayPods       int // pods deleted (not in DB)
}

// Run performs the reconciliation.
// Should be called BEFORE the HTTP server starts.
func (r *Reconciler) Run(ctx context.Context) (*Result, error) {
	r.logger.Info("starting reconciliation")

	result := &Result{}

	// Get all minions in active states
	activeMinions, err := r.minions.ListByStatuses(ctx, []db.MinionStatus{
		db.StatusPending,
		db.StatusRunning,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list active minions: %w", err)
	}

	r.logger.Info("found active minions", "count", len(activeMinions))

	// Get all pods in the namespace
	pods, err := r.pods.ListPods(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	r.logger.Info("found pods", "count", len(pods))

	// Build a map of pod name -> pod info for quick lookup
	// and a set of minion IDs that have pods
	podByName := make(map[string]k8s.PodInfo, len(pods))
	podMinionIDs := make(map[string]bool, len(pods))
	for _, pod := range pods {
		podByName[pod.Name] = pod
		if pod.MinionID != "" {
			podMinionIDs[pod.MinionID] = true
		}
	}

	// Build set of known minion IDs (from DB)
	knownMinionIDs := make(map[string]bool, len(activeMinions))
	for _, m := range activeMinions {
		knownMinionIDs[m.ID.String()] = true
	}

	// Check each active minion for orphan status
	for _, minion := range activeMinions {
		minionIDStr := minion.ID.String()
		expectedPodName := "minion-" + minionIDStr

		pod, hasPod := podByName[expectedPodName]

		// Orphan conditions:
		// 1. No pod exists for this minion
		// 2. Pod exists but is in terminal phase (Failed/Succeeded)
		isOrphaned := false
		var orphanReason string

		if !hasPod {
			isOrphaned = true
			orphanReason = "pod not found during reconciliation"
		} else if pod.Phase == "Failed" || pod.Phase == "Succeeded" {
			isOrphaned = true
			orphanReason = fmt.Sprintf("pod in terminal phase: %s", pod.Phase)
		}

		if isOrphaned {
			r.logger.Warn("marking orphaned minion as failed",
				"minion_id", minionIDStr,
				"reason", orphanReason,
			)

			if err := r.minions.MarkFailed(ctx, minion.ID, orphanReason); err != nil {
				r.logger.Error("failed to mark minion as failed",
					"minion_id", minionIDStr,
					"error", err,
				)
				// Continue with other minions
				continue
			}
			result.OrphanedMinions++
		}
	}

	// Find and delete stray pods (pods not associated with any known minion)
	for _, pod := range pods {
		// A stray pod is one where:
		// 1. It has a minion-id label but that minion doesn't exist in DB (or is already terminal)
		// 2. It has no minion-id label (shouldn't happen, but handle it)
		isStray := false

		if pod.MinionID == "" {
			// Pod has no minion-id label - this is unexpected, treat as stray
			isStray = true
		} else if !knownMinionIDs[pod.MinionID] {
			// Pod's minion ID doesn't exist in active minions
			// (could be already completed/failed/terminated, or never existed)
			isStray = true
		}

		if isStray {
			r.logger.Warn("deleting stray pod",
				"pod_name", pod.Name,
				"minion_id", pod.MinionID,
			)

			if err := r.pods.TerminatePod(ctx, pod.Name); err != nil {
				r.logger.Error("failed to delete stray pod",
					"pod_name", pod.Name,
					"error", err,
				)
				// Continue with other pods
				continue
			}
			result.StrayPods++
		}
	}

	r.logger.Info("reconciliation complete",
		"orphaned_minions_marked_failed", result.OrphanedMinions,
		"stray_pods_deleted", result.StrayPods,
	)

	return result, nil
}
