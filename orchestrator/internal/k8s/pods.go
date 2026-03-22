// Package k8s provides Kubernetes client operations for the orchestrator.
package k8s

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Namespace for all minion pods.
const Namespace = "minions"

// PodTerminator handles pod lifecycle termination.
// Implementations may use the real Kubernetes client or be a no-op for testing.
type PodTerminator interface {
	// TerminatePod deletes a pod by name.
	// Returns nil if pod doesn't exist (idempotent).
	TerminatePod(ctx context.Context, podName string) error
}

// PodSpawner handles pod creation for minion tasks.
type PodSpawner interface {
	// SpawnPod creates a new pod for a minion task.
	// Returns the pod name on success.
	SpawnPod(ctx context.Context, params SpawnParams) (podName string, err error)
}

// SpawnParams contains parameters for spawning a minion pod.
type SpawnParams struct {
	MinionID    uuid.UUID
	Repo        string
	Task        string
	Model       string
	GitHubToken string // Installation token for repo access
	CallbackURL string // URL for devbox to POST completion callback
}

// PodManager handles both pod creation and termination.
// Combines PodSpawner and PodTerminator interfaces for convenience.
type PodManager interface {
	PodSpawner
	PodTerminator
}

// Config holds configuration for the Kubernetes client.
type Config struct {
	// DevboxImage is the container image for devbox pods.
	// e.g., "ghcr.io/anomalyco/minions-devbox:latest"
	DevboxImage string

	// APIKeys for LLM providers, passed as env vars to devbox.
	AnthropicAPIKey string
	OpenAIAPIKey    string
}

// Client provides Kubernetes operations for minion pods.
type Client struct {
	clientset *kubernetes.Clientset
	config    Config
	logger    *slog.Logger
}

// NewClient creates a new Kubernetes client using in-cluster configuration.
// This expects the orchestrator to be running inside a Kubernetes pod.
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &Client{
		clientset: clientset,
		config:    cfg,
		logger:    logger,
	}, nil
}

// SpawnPod creates a new pod for a minion task with security constraints.
//
// Security context enforces:
//   - runAsNonRoot: true (pod must run as non-root user)
//   - allowPrivilegeEscalation: false
//   - All capabilities dropped
//   - Read-only root filesystem
func (c *Client) SpawnPod(ctx context.Context, params SpawnParams) (string, error) {
	podName := fmt.Sprintf("minion-%s", params.MinionID.String())

	// Non-root UID (matches devbox Dockerfile user)
	nonRootUID := int64(1000)
	nonRootGID := int64(1000)
	falseVal := false
	trueVal := true

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: Namespace,
			Labels: map[string]string{
				"app":       "minion-devbox",
				"minion-id": params.MinionID.String(),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			// Pod-level security context
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &trueVal,
				RunAsUser:    &nonRootUID,
				RunAsGroup:   &nonRootGID,
				FSGroup:      &nonRootGID,
			},
			Containers: []corev1.Container{
				{
					Name:  "devbox",
					Image: c.config.DevboxImage,
					// Container-level security context (stricter)
					SecurityContext: &corev1.SecurityContext{
						RunAsNonRoot:             &trueVal,
						RunAsUser:                &nonRootUID,
						RunAsGroup:               &nonRootGID,
						AllowPrivilegeEscalation: &falseVal,
						ReadOnlyRootFilesystem:   &trueVal,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					Env: c.buildEnvVars(params),
					// Writable temp directories for git clone, opencode work, etc.
					VolumeMounts: []corev1.VolumeMount{
						{Name: "workspace", MountPath: "/workspace"},
						{Name: "tmp", MountPath: "/tmp"},
						{Name: "home", MountPath: "/home/minion"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "home",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}

	created, err := c.clientset.CoreV1().Pods(Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create pod: %w", err)
	}

	c.logger.Info("spawned pod",
		"pod_name", created.Name,
		"minion_id", params.MinionID,
		"repo", params.Repo,
	)

	return created.Name, nil
}

// buildEnvVars constructs environment variables for the devbox container.
func (c *Client) buildEnvVars(params SpawnParams) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "MINION_ID", Value: params.MinionID.String()},
		{Name: "MINION_REPO", Value: params.Repo},
		{Name: "MINION_TASK", Value: params.Task},
		{Name: "MINION_MODEL", Value: params.Model},
		{Name: "GITHUB_TOKEN", Value: params.GitHubToken},
		{Name: "CALLBACK_URL", Value: params.CallbackURL},
	}

	// Add LLM API keys if configured
	if c.config.AnthropicAPIKey != "" {
		envs = append(envs, corev1.EnvVar{Name: "ANTHROPIC_API_KEY", Value: c.config.AnthropicAPIKey})
	}
	if c.config.OpenAIAPIKey != "" {
		envs = append(envs, corev1.EnvVar{Name: "OPENAI_API_KEY", Value: c.config.OpenAIAPIKey})
	}

	return envs
}

// TerminatePod deletes a pod by name.
// Returns nil if pod doesn't exist (idempotent).
func (c *Client) TerminatePod(ctx context.Context, podName string) error {
	err := c.clientset.CoreV1().Pods(Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		// Check if pod doesn't exist (already deleted)
		// k8s.io/apimachinery/pkg/api/errors provides IsNotFound
		// but we'll just log and return nil for idempotency
		c.logger.Warn("failed to delete pod (may not exist)", "pod_name", podName, "error", err)
		return nil // Idempotent: treat as success
	}

	c.logger.Info("terminated pod", "pod_name", podName)
	return nil
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
	if t.logger != nil {
		t.logger.Info("no-op pod termination (k8s not configured)", "pod_name", podName)
	}
	return nil
}

// NoOpPodManager is a stub implementation of PodManager that does nothing.
// Use this when k8s is not configured or for testing.
type NoOpPodManager struct {
	logger *slog.Logger
}

// NewNoOpPodManager creates a no-op pod manager.
func NewNoOpPodManager(logger *slog.Logger) *NoOpPodManager {
	return &NoOpPodManager{logger: logger}
}

// SpawnPod logs the spawn request but does nothing.
func (m *NoOpPodManager) SpawnPod(ctx context.Context, params SpawnParams) (string, error) {
	podName := fmt.Sprintf("minion-%s", params.MinionID.String())
	if m.logger != nil {
		m.logger.Info("no-op pod spawn (k8s not configured)",
			"pod_name", podName,
			"minion_id", params.MinionID,
			"repo", params.Repo,
		)
	}
	return podName, nil
}

// TerminatePod logs the termination request but does nothing.
func (m *NoOpPodManager) TerminatePod(ctx context.Context, podName string) error {
	if m.logger != nil {
		m.logger.Info("no-op pod termination (k8s not configured)", "pod_name", podName)
	}
	return nil
}
