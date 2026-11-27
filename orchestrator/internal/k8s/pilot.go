package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PilotStage describes a deterministic pod and the data mounted in it.
type PilotStage struct {
	ID                                uuid.UUID
	Step, Input, Base, Tree           string
	Model, ProviderVariable           string
	GitHubToken                       string
	Repository, Target, ProviderValue string
}

// PilotObservation describes the state of a stage pod.
type PilotObservation struct {
	Phase, Output, Result string
}

// ObservePilot adopts an existing stage pod or creates it on the first observation.
func (c *Client) ObservePilot(ctx context.Context, stage PilotStage, image string, stageToken func(context.Context) (string, error)) (PilotObservation, error) {
	var observation PilotObservation
	name := pilotPodName(stage.ID, stage.Step)
	pods := c.clientset.CoreV1().Pods(Namespace)
	existing, err := pods.Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		stage.GitHubToken, err = stageToken(ctx)
		if err != nil {
			return observation, err
		}
		return observation, c.createPilot(ctx, stage, image)
	}
	if err != nil {
		return observation, err
	}
	if existing.Labels["minion-id"] != stage.ID.String() || existing.Labels["app"] != "minion-workflow" {
		return observation, fmt.Errorf("stage pod %s does not belong to this run", name)
	}
	observation.Phase = string(existing.Status.Phase)
	if len(existing.Status.ContainerStatuses) == 1 {
		if result := existing.Status.ContainerStatuses[0].State.Terminated; result != nil {
			observation.Result = result.Message
		}
	}
	if existing.Status.Phase == core.PodSucceeded && stage.Step == "implement" {
		limit := int64(250000)
		raw, err := pods.GetLogs(name, &core.PodLogOptions{Container: "stage", LimitBytes: &limit}).DoRaw(ctx)
		if err != nil {
			return observation, err
		}
		observation.Output = string(raw)
	}
	return observation, nil
}

// DeletePilot removes stage resources after a checkpoint is durable.
func (c *Client) DeletePilot(ctx context.Context, id uuid.UUID, step string) error {
	name := pilotPodName(id, step)
	if err := c.clientset.CoreV1().Pods(Namespace).Delete(ctx, name, meta.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := c.clientset.CoreV1().ConfigMaps(Namespace).Delete(ctx, name, meta.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// createPilot creates an immutable stage input and a pod without cluster credentials.
func (c *Client) createPilot(ctx context.Context, stage PilotStage, image string) error {
	name := pilotPodName(stage.ID, stage.Step)
	labels := map[string]string{"app": "minion-workflow", "minion-id": stage.ID.String(), "workflow-step": stage.Step}
	dataKey := "patch.diff"
	if stage.Step == "implement" {
		dataKey = "task.txt"
	}
	on, off := true, false
	maps := c.clientset.CoreV1().ConfigMaps(Namespace)
	_, err := maps.Create(ctx, &core.ConfigMap{
		ObjectMeta: meta.ObjectMeta{Name: name, Labels: labels},
		Data:       map[string]string{dataKey: stage.Input}, Immutable: &on,
	}, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		var existing *core.ConfigMap
		existing, err = maps.Get(ctx, name, meta.GetOptions{})
		if err == nil && (existing.Labels["minion-id"] != stage.ID.String() || existing.Data[dataKey] != stage.Input) {
			return fmt.Errorf("stage input does not match %s", name)
		}
	}
	if err != nil {
		return err
	}

	var identity int64 = 1000
	var deadline int64 = 300
	env := []core.EnvVar{
		{Name: "WORKFLOW_BASE", Value: stage.Base},
		{Name: "WORKFLOW_REPO", Value: stage.Repository},
		{Name: "WORKFLOW_TARGET", Value: stage.Target},
	}
	fetchEnv := append(append([]core.EnvVar(nil), env...),
		core.EnvVar{Name: "WORKFLOW_STEP", Value: "fetch"},
		core.EnvVar{Name: "GH_TOKEN", Value: stage.GitHubToken})
	env = append(env, core.EnvVar{Name: "WORKFLOW_STEP", Value: stage.Step})
	switch stage.Step {
	case "implement":
		deadline = 1800
		env = append(env, core.EnvVar{Name: "OPENCODE_MODEL", Value: stage.Model})
		if stage.ProviderVariable != "" {
			env = append(env, core.EnvVar{Name: stage.ProviderVariable, Value: stage.ProviderValue})
		}
	case "publish":
		env = append(env, core.EnvVar{Name: "GH_TOKEN", Value: stage.GitHubToken},
			core.EnvVar{Name: "WORKFLOW_ID", Value: stage.ID.String()},
			core.EnvVar{Name: "WORKFLOW_TREE", Value: stage.Tree})
	}
	pod := &core.Pod{
		ObjectMeta: meta.ObjectMeta{Name: name, Labels: labels},
		Spec: core.PodSpec{
			ActiveDeadlineSeconds: &deadline, AutomountServiceAccountToken: &off, EnableServiceLinks: &off,
			RestartPolicy: core.RestartPolicyNever,
			SecurityContext: &core.PodSecurityContext{
				RunAsNonRoot: &on, RunAsUser: &identity, RunAsGroup: &identity, FSGroup: &identity,
				SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []core.Container{{
				Name: "stage", Image: image, ImagePullPolicy: core.PullAlways, Command: []string{"/opt/minions/workflow.sh"}, Env: env,
				SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: &off, ReadOnlyRootFilesystem: &on,
					Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}},
				Resources: core.ResourceRequirements{
					Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("256Mi")},
					Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("2"), core.ResourceMemory: resource.MustParse("2Gi"),
						core.ResourceEphemeralStorage: resource.MustParse("2Gi")},
				},
				VolumeMounts: []core.VolumeMount{{Name: "workspace", MountPath: "/workspace"}, {Name: "tmp", MountPath: "/tmp"},
					{Name: "input", MountPath: "/task", ReadOnly: true}},
			}},
			Volumes: []core.Volume{
				{Name: "workspace", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{}}},
				{Name: "tmp", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{}}},
				{Name: "input", VolumeSource: core.VolumeSource{ConfigMap: &core.ConfigMapVolumeSource{LocalObjectReference: core.LocalObjectReference{Name: name}}}},
			},
		},
	}
	if stage.Step != "publish" {
		fetch := pod.Spec.Containers[0]
		fetch.Name, fetch.Env = "fetch", fetchEnv
		fetch.VolumeMounts = fetch.VolumeMounts[:1]
		pod.Spec.InitContainers = []core.Container{fetch}
	}
	_, err = c.clientset.CoreV1().Pods(Namespace).Create(ctx, pod, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// pilotPodName returns a stable name that survives retries after an ambiguous API response.
func pilotPodName(id uuid.UUID, stage string) string {
	return "workflow-" + strings.TrimSpace(stage) + "-" + id.String()
}
