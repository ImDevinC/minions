// Package k8s provides Kubernetes client operations for the orchestrator.
package k8s

import (
	"context"
	"log/slog"
)

// PodTerminator handles pod lifecycle termination.
// Implementations may use the real Kubernetes client or be a no-op for testing.
type PodTerminator interface {
	// TerminatePod deletes a pod by name.
	// Returns nil if pod doesn't exist (idempotent).
	TerminatePod(ctx context.Context, podName string) error
}

// NoOpPodTerminator is a stub implementation that does nothing.
// Use this when k8s is not configured or for testing.
type NoOpPodTerminator struct {
	logger *slog.Logger
}

// NewNoOpPodTerminator creates a no-op pod terminator.
func NewNoOpPodTerminator(logger *slog.Logger) *NoOpPodTerminator {
	return &NoOpPodTerminator{logger: logger}
}

// TerminatePod logs the termination request but does nothing.
func (t *NoOpPodTerminator) TerminatePod(ctx context.Context, podName string) error {
	t.logger.Info("no-op pod termination (k8s not configured)", "pod_name", podName)
	return nil
}
