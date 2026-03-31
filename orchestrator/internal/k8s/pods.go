// Package k8s provides Kubernetes client operations for the orchestrator.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Namespace for all minion pods.
const Namespace = "minions"

// TaskConfigMapKey is the key used to store the task content in the ConfigMap.
const TaskConfigMapKey = "task.txt"

// TaskMountPath is where the task ConfigMap is mounted in the pod.
const TaskMountPath = "/task"

// Retry configuration for pod creation.
const (
	// MaxRetries is the number of retry attempts for pod creation (total attempts = MaxRetries + 1).
	MaxRetries = 3
	// InitialBackoff is the initial backoff duration before the first retry.
	InitialBackoff = 1 * time.Second
	// MaxBackoff caps the backoff duration.
	MaxBackoff = 30 * time.Second
	// BackoffMultiplier is the factor by which backoff increases each retry.
	BackoffMultiplier = 2
)

// ErrRetriesExhausted is returned when all pod creation retries have failed.
var ErrRetriesExhausted = errors.New("pod creation failed after all retries")

// ErrPodTimeout is returned when a pod does not become ready within the timeout.
var ErrPodTimeout = errors.New("pod creation timeout: pod did not become ready within deadline")

// Pod readiness timeout configuration.
const (
	// PodReadyTimeout is the maximum time to wait for a pod to become ready.
	PodReadyTimeout = 5 * time.Minute
	// PodPollInterval is how often to check pod status while waiting.
	PodPollInterval = 2 * time.Second
)

// PodInfo contains basic information about a pod.
type PodInfo struct {
	Name     string
	MinionID string // extracted from label "minion-id"
	Phase    string // Running, Pending, Failed, Succeeded, Unknown
}

// PodTerminator handles pod lifecycle termination.
// Implementations may use the real Kubernetes client or be a no-op for testing.
type PodTerminator interface {
	// TerminatePod deletes a pod by name.
	// Returns nil if pod doesn't exist (idempotent).
	TerminatePod(ctx context.Context, podName string) error
}

// PodIPProvider resolves pod IP addresses.
type PodIPProvider interface {
	// GetPodIP returns the IP address of a running pod.
	// Returns error if pod doesn't exist or doesn't have an IP yet.
	GetPodIP(ctx context.Context, podName string) (string, error)
}

// PodLister can list pods in the namespace.
type PodLister interface {
	// ListPods returns all minion pods in the namespace.
	ListPods(ctx context.Context) ([]PodInfo, error)
}

// PodSpawner handles pod creation for minion tasks.
type PodSpawner interface {
	// SpawnPod creates a new pod for a minion task.
	// Returns the pod name on success.
	SpawnPod(ctx context.Context, params SpawnParams) (podName string, err error)

	// SpawnPodWithRetry creates a pod with exponential backoff retry.
	// Retries up to MaxRetries times before returning ErrRetriesExhausted.
	SpawnPodWithRetry(ctx context.Context, params SpawnParams) (podName string, err error)

	// WaitForPodReady waits up to PodReadyTimeout for a pod to become ready.
	// If the timeout is exceeded, the pod is deleted and ErrPodTimeout is returned.
	WaitForPodReady(ctx context.Context, podName string) error
}

// SpawnParams contains parameters for spawning a minion pod.
type SpawnParams struct {
	MinionID         uuid.UUID
	Repo             string
	Task             string
	Model            string
	GitHubToken      string // Installation token for repo access
	OrchestratorURL  string // Base URL for orchestrator (callbacks, etc.)
	InternalAPIToken string // Token for authenticating with orchestrator
}

// PodManager handles both pod creation and termination.
// Combines PodSpawner, PodTerminator, PodLister, and PodIPProvider interfaces for convenience.
type PodManager interface {
	PodSpawner
	PodTerminator
	PodLister
	PodIPProvider
}

// Config holds configuration for the Kubernetes client.
type Config struct {
	// DevboxImage is the container image for devbox pods.
	// e.g., "ghcr.io/imdevinc/minions/devbox:latest"
	DevboxImage string

	// AuthPVCName is the name of the PVC containing auth.json for OpenCode.
	// If empty, no auth PVC is mounted.
	AuthPVCName string
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

// taskConfigMapName returns the ConfigMap name for a minion's task.
func taskConfigMapName(minionID string) string {
	return fmt.Sprintf("minion-task-%s", minionID)
}

// createTaskConfigMap creates a ConfigMap containing the task content.
// Returns the created ConfigMap or an error.
func (c *Client) createTaskConfigMap(ctx context.Context, minionID, task string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskConfigMapName(minionID),
			Namespace: Namespace,
			Labels: map[string]string{
				"app":       "minion-devbox",
				"minion-id": minionID,
			},
		},
		Data: map[string]string{
			TaskConfigMapKey: task,
		},
	}

	created, err := c.clientset.CoreV1().ConfigMaps(Namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create task ConfigMap: %w", err)
	}

	c.logger.Info("created task ConfigMap",
		"configmap_name", created.Name,
		"minion_id", minionID,
	)

	return created, nil
}

// deleteTaskConfigMap deletes the task ConfigMap for a minion.
// Returns nil if ConfigMap doesn't exist (idempotent).
func (c *Client) deleteTaskConfigMap(ctx context.Context, minionID string) error {
	cmName := taskConfigMapName(minionID)
	err := c.clientset.CoreV1().ConfigMaps(Namespace).Delete(ctx, cmName, metav1.DeleteOptions{})
	if err != nil {
		// Treat as success if not found (already deleted)
		c.logger.Warn("failed to delete task ConfigMap (may not exist)", "configmap_name", cmName, "error", err)
		return nil
	}

	c.logger.Info("deleted task ConfigMap", "configmap_name", cmName, "minion_id", minionID)
	return nil
}

// SpawnPod creates a new pod for a minion task with security constraints.
// It first creates a ConfigMap containing the task, then creates the pod
// with the ConfigMap mounted as a volume.
//
// Security context enforces:
//   - runAsNonRoot: true (pod must run as non-root user)
//   - allowPrivilegeEscalation: false
//   - All capabilities dropped
//   - Read-only root filesystem
func (c *Client) SpawnPod(ctx context.Context, params SpawnParams) (string, error) {
	minionIDStr := params.MinionID.String()
	podName := fmt.Sprintf("minion-%s", minionIDStr)

	// Create ConfigMap with task content first
	_, err := c.createTaskConfigMap(ctx, minionIDStr, params.Task)
	if err != nil {
		return "", err
	}

	// Non-root UID (matches devbox Dockerfile user)
	nonRootUID := int64(1000)
	nonRootGID := int64(1000)
	falseVal := false
	trueVal := true

	// Build volume mounts - base mounts plus optional auth PVC
	volumeMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "home", MountPath: "/home/minion"},
		{Name: "task", MountPath: TaskMountPath, ReadOnly: true},
	}

	// Build volumes - base volumes plus optional auth PVC
	volumes := []corev1.Volume{
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
		{
			Name: "task",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: taskConfigMapName(minionIDStr),
					},
				},
			},
		},
	}

	// Add auth PVC mount if configured
	// Mount the PVC to /etc/opencode-share and let the entrypoint symlink auth.json
	// to ~/.local/share/opencode/auth.json. This avoids subPath permission issues
	// that prevent OpenCode from creating sibling directories (log/, state/, etc.).
	if c.config.AuthPVCName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "auth-pvc",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: c.config.AuthPVCName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "auth-pvc",
			MountPath: "/etc/opencode-share",
		})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: Namespace,
			Labels: map[string]string{
				"app":       "minion-devbox",
				"minion-id": minionIDStr,
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
					Name:            "devbox",
					Image:           c.config.DevboxImage,
					ImagePullPolicy: corev1.PullAlways,
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
					// Task is mounted read-only from ConfigMap.
					VolumeMounts: volumeMounts,
				},
			},
			Volumes: volumes,
		},
	}

	created, err := c.clientset.CoreV1().Pods(Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		// Rollback: delete the ConfigMap since pod creation failed
		c.logger.Warn("pod creation failed, rolling back ConfigMap",
			"minion_id", minionIDStr,
			"error", err,
		)
		_ = c.deleteTaskConfigMap(ctx, minionIDStr)
		return "", fmt.Errorf("failed to create pod: %w", err)
	}

	c.logger.Info("spawned pod",
		"pod_name", created.Name,
		"minion_id", params.MinionID,
		"repo", params.Repo,
	)

	return created.Name, nil
}

// SpawnPodWithRetry creates a pod with exponential backoff retry.
// Retries up to MaxRetries times before returning ErrRetriesExhausted.
// Each retry logs the failure and waits with exponential backoff before retrying.
func (c *Client) SpawnPodWithRetry(ctx context.Context, params SpawnParams) (string, error) {
	var lastErr error
	backoff := InitialBackoff

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		// Attempt to spawn the pod
		podName, err := c.SpawnPod(ctx, params)
		if err == nil {
			return podName, nil
		}

		lastErr = err
		c.logger.Warn("pod creation failed, will retry",
			"minion_id", params.MinionID,
			"attempt", attempt+1,
			"max_attempts", MaxRetries+1,
			"error", err,
			"backoff", backoff,
		)

		// Don't wait after the last attempt
		if attempt == MaxRetries {
			break
		}

		// Wait with backoff, respecting context cancellation
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(backoff):
		}

		// Exponential backoff with cap
		backoff *= BackoffMultiplier
		if backoff > MaxBackoff {
			backoff = MaxBackoff
		}
	}

	c.logger.Error("pod creation failed after all retries",
		"minion_id", params.MinionID,
		"total_attempts", MaxRetries+1,
		"last_error", lastErr,
	)

	return "", fmt.Errorf("%w: %v", ErrRetriesExhausted, lastErr)
}

// WaitForPodReady waits up to PodReadyTimeout for a pod to become ready.
// If the timeout is exceeded, the pod is deleted and ErrPodTimeout is returned.
// A pod is considered ready when it has a Running phase and all containers
// have the Ready condition set to true.
func (c *Client) WaitForPodReady(ctx context.Context, podName string) error {
	// Create a timeout context if not already bounded
	timeoutCtx, cancel := context.WithTimeout(ctx, PodReadyTimeout)
	defer cancel()

	c.logger.Info("waiting for pod to become ready",
		"pod_name", podName,
		"timeout", PodReadyTimeout,
	)

	ticker := time.NewTicker(PodPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			// Timeout exceeded, delete the pod
			c.logger.Error("pod creation timeout exceeded, deleting pod",
				"pod_name", podName,
				"timeout", PodReadyTimeout,
			)
			// Best-effort deletion, ignore errors
			_ = c.TerminatePod(context.Background(), podName)
			return ErrPodTimeout

		case <-ticker.C:
			pod, err := c.clientset.CoreV1().Pods(Namespace).Get(timeoutCtx, podName, metav1.GetOptions{})
			if err != nil {
				c.logger.Warn("failed to get pod status",
					"pod_name", podName,
					"error", err,
				)
				continue
			}

			// Check pod phase
			switch pod.Status.Phase {
			case corev1.PodRunning:
				// Check if all containers are ready
				if isPodReady(pod) {
					c.logger.Info("pod is ready",
						"pod_name", podName,
					)
					return nil
				}
				c.logger.Debug("pod is running but not all containers ready",
					"pod_name", podName,
				)

			case corev1.PodFailed, corev1.PodSucceeded:
				// Pod terminated before becoming ready
				c.logger.Error("pod terminated unexpectedly",
					"pod_name", podName,
					"phase", pod.Status.Phase,
					"reason", pod.Status.Reason,
				)
				return fmt.Errorf("pod terminated with phase %s: %s", pod.Status.Phase, pod.Status.Reason)

			case corev1.PodPending:
				// Still waiting for scheduling/image pull
				c.logger.Debug("pod still pending",
					"pod_name", podName,
				)

			default:
				c.logger.Debug("pod in unknown phase",
					"pod_name", podName,
					"phase", pod.Status.Phase,
				)
			}
		}
	}
}

// isPodReady checks if all containers in the pod have the Ready condition.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// buildEnvVars constructs environment variables for the devbox container.
// Note: Task content is passed via ConfigMap volume mount, not environment variable.
// All orchestrator env vars with DEVBOX_ prefix are passed through with the prefix stripped.
func (c *Client) buildEnvVars(params SpawnParams) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "MINION_ID", Value: params.MinionID.String()},
		{Name: "MINION_REPO", Value: params.Repo},
		{Name: "OPENCODE_MODEL", Value: params.Model},
		{Name: "GITHUB_TOKEN", Value: params.GitHubToken},
		{Name: "ORCHESTRATOR_URL", Value: params.OrchestratorURL},
		{Name: "INTERNAL_API_TOKEN", Value: params.InternalAPIToken},
	}

	// Pass through all DEVBOX_* env vars with prefix stripped
	// e.g., DEVBOX_OPENROUTER_API_KEY -> OPENROUTER_API_KEY
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DEVBOX_") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				name := strings.TrimPrefix(parts[0], "DEVBOX_")
				envs = append(envs, corev1.EnvVar{Name: name, Value: parts[1]})
			}
		}
	}

	return envs
}

// TerminatePod deletes a pod and its associated task ConfigMap by minion ID.
// Returns nil if pod/ConfigMap doesn't exist (idempotent).
func (c *Client) TerminatePod(ctx context.Context, podName string) error {
	// Extract minion ID from pod name (format: "minion-<uuid>")
	minionID := ""
	if len(podName) > 7 && podName[:7] == "minion-" {
		minionID = podName[7:]
	}

	// Delete the pod
	err := c.clientset.CoreV1().Pods(Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		// Check if pod doesn't exist (already deleted)
		// k8s.io/apimachinery/pkg/api/errors provides IsNotFound
		// but we'll just log and return nil for idempotency
		c.logger.Warn("failed to delete pod (may not exist)", "pod_name", podName, "error", err)
	} else {
		c.logger.Info("terminated pod", "pod_name", podName)
	}

	// Delete the associated ConfigMap if we have the minion ID
	if minionID != "" {
		_ = c.deleteTaskConfigMap(ctx, minionID)
	}

	return nil
}

// ListPods returns all minion pods in the namespace.
func (c *Client) ListPods(ctx context.Context) ([]PodInfo, error) {
	pods, err := c.clientset.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=minion-devbox",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	result := make([]PodInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		info := PodInfo{
			Name:     pod.Name,
			MinionID: pod.Labels["minion-id"],
			Phase:    string(pod.Status.Phase),
		}
		result = append(result, info)
	}

	return result, nil
}

// ErrPodNotFound is returned when a pod does not exist.
var ErrPodNotFound = errors.New("pod not found")

// ErrPodNoIP is returned when a pod exists but doesn't have an IP assigned yet.
var ErrPodNoIP = errors.New("pod has no IP assigned")

// GetPodIP returns the IP address of a running pod.
// Returns ErrPodNotFound if pod doesn't exist, ErrPodNoIP if no IP yet.
func (c *Client) GetPodIP(ctx context.Context, podName string) (string, error) {
	pod, err := c.clientset.CoreV1().Pods(Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		// TODO: use apierrors.IsNotFound(err) for proper detection
		return "", fmt.Errorf("%w: %s", ErrPodNotFound, podName)
	}

	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("%w: %s (phase: %s)", ErrPodNoIP, podName, pod.Status.Phase)
	}

	return pod.Status.PodIP, nil
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

// SpawnPodWithRetry logs the spawn request but does nothing.
// No-op implementation always succeeds on first attempt.
func (m *NoOpPodManager) SpawnPodWithRetry(ctx context.Context, params SpawnParams) (string, error) {
	return m.SpawnPod(ctx, params)
}

// WaitForPodReady logs the wait request and returns immediately.
// No-op implementation simulates instant readiness.
func (m *NoOpPodManager) WaitForPodReady(ctx context.Context, podName string) error {
	if m.logger != nil {
		m.logger.Info("no-op pod ready wait (k8s not configured)", "pod_name", podName)
	}
	return nil
}

// TerminatePod logs the termination request but does nothing.
func (m *NoOpPodManager) TerminatePod(ctx context.Context, podName string) error {
	if m.logger != nil {
		m.logger.Info("no-op pod termination (k8s not configured)", "pod_name", podName)
	}
	return nil
}

// ListPods returns an empty list (no k8s configured).
func (m *NoOpPodManager) ListPods(ctx context.Context) ([]PodInfo, error) {
	if m.logger != nil {
		m.logger.Info("no-op pod list (k8s not configured)")
	}
	return []PodInfo{}, nil
}

// GetPodIP returns a fake IP for testing.
func (m *NoOpPodManager) GetPodIP(ctx context.Context, podName string) (string, error) {
	if m.logger != nil {
		m.logger.Info("no-op pod IP lookup (k8s not configured)", "pod_name", podName)
	}
	// Return localhost for testing purposes
	return "127.0.0.1", nil
}
