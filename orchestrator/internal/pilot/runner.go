// Package pilot runs the default staged maintenance workflow.
package pilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/imdevinc/minions/orchestrator/internal/db"
	"github.com/imdevinc/minions/orchestrator/internal/github"
	"github.com/imdevinc/minions/orchestrator/internal/k8s"
	"github.com/imdevinc/minions/orchestrator/internal/webhook"
)

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Runner observes each durable stage and advances it after recording the result.
type Runner struct {
	Store    *db.MinionStore
	Pods     *k8s.Client
	GitHub   *github.Manager
	Notifier webhook.Notifier
	Logger   *slog.Logger
	Image    string
}

// Run processes ready checkpoints until the context is canceled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
			r.cleanup(ctx)
		}
	}
}

// cleanup retries deletion of a finished run's resources after a restart.
func (r *Runner) cleanup(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := r.Store.NextPilotCleanup(ctx)
	if err == nil && id != uuid.Nil {
		for _, step := range []string{"implement", "verify", "publish"} {
			if err = r.Pods.DeletePilot(ctx, id, step); err != nil {
				break
			}
		}
		if err == nil {
			err = r.Store.MarkPilotCleaned(ctx, id)
		}
	}
	if err != nil {
		r.Logger.Warn("workflow resource cleanup will retry", "run", id, "error", err)
	}
}

// tick locks a run while it reconciles one external operation.
func (r *Runner) tick(ctx context.Context) {
	stepContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	run, update, err := r.Store.AdvancePilot(stepContext, func(run db.PilotRun) (db.PilotUpdate, error) {
		operationContext, cancel := context.WithTimeout(stepContext, 20*time.Second)
		defer cancel()
		update, err := r.advance(operationContext, run)
		if err == nil {
			return update, nil
		}
		if stepContext.Err() != nil {
			return db.PilotUpdate{}, err
		}
		r.Logger.Warn("workflow operation will be reconciled", "run", run.ID, "step", run.Step, "error", err)
		if time.Since(run.ChangedAt) > 40*time.Minute || run.Status == db.StatusTerminated {
			return failure("Could not reconcile the workflow operation. Check the run branch on GitHub before retrying."), nil
		}
		return db.PilotUpdate{Delay: 20 * time.Second}, nil
	})
	if err != nil {
		r.Logger.Error("workflow checkpoint failed", "error", err)
		return
	}
	if run.ID == uuid.Nil {
		return
	}
	if update.Step != run.Step && run.Step != "ci" {
		if err := r.Pods.DeletePilot(stepContext, run.ID, run.Step); err != nil {
			r.Logger.Warn("workflow cleanup failed", "run", run.ID, "step", run.Step, "error", err)
		}
	}
	if (run.Channel == nil && run.Room == nil) || (!update.Complete && update.PR == "") {
		return
	}
	if run.Status == db.StatusTerminated && update.PR == "" {
		return
	}
	note := webhook.Notification{MinionID: run.ID, Platform: webhook.Platform(run.Platform), Type: webhook.NotifyCompleted, PRURL: update.PR}
	if note.PRURL == "" && run.PR != nil {
		note.PRURL = *run.PR
	}
	if run.Channel != nil {
		note.DiscordChannelID = *run.Channel
	}
	if run.Room != nil {
		note.MatrixRoomID = *run.Room
	}
	switch {
	case run.Status == db.StatusTerminated:
		note.Summary = "Run canceled. GitHub had already accepted the draft PR."
	case update.Failure != "":
		note.Type, note.Error = webhook.NotifyFailed, update.Failure
	case update.Step == "ci":
		note.Type = webhook.NotifyDraftOpened
		note.Summary = run.Evidence + " Waiting for GitHub checks."
	default:
		note.Summary = update.Evidence
	}
	if err := r.Notifier.Notify(stepContext, note); err != nil {
		r.Logger.Warn("workflow notification failed", "run", run.ID, "error", err)
	}
}

// advance observes the current stage and never runs repository code in the publisher.
func (r *Runner) advance(ctx context.Context, run db.PilotRun) (db.PilotUpdate, error) {
	branch := "minion/" + run.ID.String()
	if run.Status == db.StatusTerminated {
		update := db.PilotUpdate{Complete: true}
		if run.Step == "publish" && run.Head != "" {
			var err error
			update.PR, err = r.GitHub.WorkflowDraft(ctx, run.Repo, run.Target, branch, run.Base, run.Head, "", false)
			if err != nil {
				return db.PilotUpdate{}, err
			}
		}
		// The main-row lock serializes cancellation against PR creation.
		return update, nil
	}
	if run.Step != "ci" && time.Since(run.ChangedAt) > 40*time.Minute &&
		!(run.Step == "implement" && run.Status == db.StatusPending) {
		return failure("Workflow stage exceeded its time limit. Check the run branch before retrying."), nil
	}
	if run.Step == "ci" {
		if time.Since(run.ChangedAt) >= 25*time.Minute {
			return failure("Draft PR remains open. GitHub checks did not report a result in time."), nil
		}
		conclusion, err := r.GitHub.WorkflowCheck(ctx, run.Repo, branch, run.Head)
		if err != nil {
			return db.PilotUpdate{}, err
		}
		switch conclusion {
		case "":
			return db.PilotUpdate{Delay: 90 * time.Second}, nil
		case "success":
			return db.PilotUpdate{Complete: true, Evidence: run.Evidence + " Observed GitHub checks passed."}, nil
		default:
			return failure(fmt.Sprintf("Draft PR remains open. GitHub CI: %s.", conclusion)), nil
		}
	}
	if run.Step == "publish" && run.Head != "" {
		pr, err := r.GitHub.WorkflowDraft(ctx, run.Repo, run.Target, branch, run.Base, run.Head, run.Evidence, true)
		if err != nil {
			return db.PilotUpdate{}, err
		}
		return db.PilotUpdate{Step: "ci", PR: pr, Delay: 10 * time.Second}, nil
	}
	if run.Target == "" {
		target, err := r.GitHub.WorkflowRepository(ctx, run.Repo)
		return db.PilotUpdate{Target: target}, err
	}
	stage := k8s.PilotStage{ID: run.ID, Step: run.Step, Input: run.Patch, Base: run.Base, Tree: run.Tree,
		Repository: run.Repo, Target: run.Target}
	access := "read"
	if run.Step == "implement" {
		stage.Input, stage.Model = run.Task, run.Model
		provider, _, _ := strings.Cut(run.Model, "/")
		key := strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
		if key != "_API_KEY" && strings.IndexFunc(key, func(c rune) bool { return !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789", c) }) == -1 {
			if value := os.Getenv("DEVBOX_" + key); value != "" {
				stage.ProviderVariable, stage.ProviderValue = key, value
			}
		}
	} else if run.Step == "publish" {
		access = "push"
	}
	observation, err := r.Pods.ObservePilot(ctx, stage, r.Image, func(ctx context.Context) (string, error) {
		return r.GitHub.WorkflowToken(ctx, run.Repo, access)
	})
	if err != nil {
		return db.PilotUpdate{}, err
	}
	switch observation.Phase {
	case "Failed":
		if run.Step == "verify" && observation.Result == "unsupported" {
			return failure("No supported local verification profile for this change. No PR was opened."), nil
		}
		return failure("The " + run.Step + " stage failed. No additional publication was attempted."), nil
	case "Succeeded":
		switch run.Step {
		case "implement":
			return candidate(observation.Output), nil
		case "verify":
			profile, tree, _ := strings.Cut(observation.Result, ":")
			evidence := map[string]string{"go": "Local Go tests passed.", "npm": "Local npm tests passed.",
				"docs": "Documentation diff check passed. No application tests ran."}[profile]
			if evidence != "" && sha.MatchString(tree) {
				return db.PilotUpdate{Step: "publish", Tree: tree, Evidence: evidence}, nil
			}
		case "publish":
			if sha.MatchString(observation.Result) {
				return db.PilotUpdate{Head: observation.Result}, nil
			}
		}
		return failure("Stage returned invalid evidence."), nil
	default:
		return db.PilotUpdate{Delay: 6 * time.Second}, nil
	}
}

// candidate decodes and bounds the untrusted patch artifact.
func candidate(output string) db.PilotUpdate {
	header, patch, ok := strings.Cut(output, "\n")
	base, digest, hasDigest := strings.Cut(header, ":")
	if !ok || !hasDigest || !sha.MatchString(base) || len(patch) > 240000 || !utf8.ValidString(patch) || strings.ContainsRune(patch, 0) {
		return failure("Implementation returned an invalid patch.")
	}
	sum := sha256.Sum256([]byte(patch))
	if hex.EncodeToString(sum[:]) != digest {
		return failure("Implementation returned an incomplete or changed patch.")
	}
	if patch == "" {
		return db.PilotUpdate{Complete: true, Evidence: "No changes to publish."}
	}
	if !strings.HasPrefix(patch, "diff --git ") {
		return failure("Implementation returned an invalid patch.")
	}
	return db.PilotUpdate{Step: "verify", Base: base, Patch: patch}
}

// failure returns a terminal checkpoint with a user-facing reason.
func failure(reason string) db.PilotUpdate {
	return db.PilotUpdate{Complete: true, Failure: reason}
}
