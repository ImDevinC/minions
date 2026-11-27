// Package pilot runs the go-fifa maintenance workflow.
package pilot

import (
	"context"
	"fmt"
	"log/slog"
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
	Store          *db.MinionStore
	Pods           *k8s.Client
	GitHub         *github.Manager
	Notifier       webhook.Notifier
	Logger         *slog.Logger
	Image          string
	ProviderSecret string
	ProviderKey    string
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
		r.Logger.Warn("pilot resource cleanup will retry", "run", id, "error", err)
	}
}

// tick locks a run while it reconciles one external operation.
func (r *Runner) tick(ctx context.Context) {
	stepContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	run, update, err := r.Store.AdvancePilot(stepContext, func(run db.PilotRun) (db.PilotUpdate, error) {
		update, err := r.advance(stepContext, run)
		if err == nil {
			return update, nil
		}
		if stepContext.Err() != nil {
			return db.PilotUpdate{}, err
		}
		r.Logger.Warn("pilot operation will be reconciled", "run", run.ID, "step", run.Step, "error", err)
		if time.Since(run.ChangedAt) > 40*time.Minute || run.Status == db.StatusTerminated {
			return failure("Could not reconcile the workflow operation. Check the run branch on GitHub before retrying."), nil
		}
		return db.PilotUpdate{Delay: 20 * time.Second}, nil
	})
	if err != nil {
		r.Logger.Error("pilot checkpoint failed", "error", err)
		return
	}
	if run.ID == uuid.Nil {
		return
	}
	if update.Step != run.Step && run.Step != "ci" {
		if err := r.Pods.DeletePilot(stepContext, run.ID, run.Step); err != nil {
			r.Logger.Warn("pilot cleanup failed", "run", run.ID, "step", run.Step, "error", err)
		}
	}
	if run.Channel == nil || (!update.Complete && update.PR == "") {
		return
	}
	if run.Status == db.StatusTerminated && update.PR == "" {
		return
	}
	note := webhook.Notification{MinionID: run.ID, DiscordChannelID: *run.Channel, Platform: webhook.PlatformDiscord,
		Type: webhook.NotifyCompleted, PRURL: update.PR}
	switch {
	case run.Status == db.StatusTerminated:
		note.Summary = "Run canceled. GitHub had already accepted the draft PR."
	case update.Failure != "":
		note.Type, note.Error = webhook.NotifyFailed, update.Failure
	case update.Step == "ci":
		note.Summary = "Draft PR opened. Local tests passed. Waiting for GitHub Actions."
	default:
		note.Summary = update.Evidence
	}
	if err := r.Notifier.Notify(stepContext, note); err != nil {
		r.Logger.Warn("pilot notification failed", "run", run.ID, "error", err)
	}
}

// advance observes the current stage and never runs repository code in the publisher.
func (r *Runner) advance(ctx context.Context, run db.PilotRun) (db.PilotUpdate, error) {
	branch := "minion/" + run.ID.String()
	if run.Status == db.StatusTerminated {
		update := db.PilotUpdate{Complete: true}
		if run.Step == "publish" && run.Head != "" {
			var err error
			update.PR, err = r.GitHub.FindPilotDraft(ctx, branch, run.Head)
			if err != nil {
				return db.PilotUpdate{}, err
			}
		}
		// The main-row lock serializes cancellation against PR creation.
		return update, nil
	}
	if run.Step != "ci" && time.Since(run.ChangedAt) > 40*time.Minute &&
		!(run.Step == "implement" && run.Status == db.StatusPending) {
		return failure("Pilot stage exceeded its time limit. Check the run branch before retrying."), nil
	}
	if run.Step == "ci" {
		if time.Since(run.ChangedAt) >= 25*time.Minute {
			return failure("Draft PR remains open. GitHub CI did not report the required check in time."), nil
		}
		conclusion, err := r.GitHub.PilotCheck(ctx, branch, run.Head)
		if err != nil {
			return db.PilotUpdate{}, err
		}
		switch conclusion {
		case "":
			return db.PilotUpdate{Delay: 90 * time.Second}, nil
		case "success":
			return db.PilotUpdate{Complete: true, Evidence: "Local validation and GitHub Actions passed."}, nil
		default:
			return failure(fmt.Sprintf("Draft PR remains open. GitHub CI: %s.", conclusion)), nil
		}
	}
	if run.Step == "publish" && run.Head != "" {
		pr, err := r.GitHub.PilotDraft(ctx, branch, run.Base, run.Head)
		if err != nil {
			return db.PilotUpdate{}, err
		}
		return db.PilotUpdate{Step: "ci", PR: pr, Delay: 10 * time.Second}, nil
	}
	stage := k8s.PilotStage{ID: run.ID, Step: run.Step, Input: run.Patch, Base: run.Base, Tree: run.Tree}
	if run.Step == "implement" {
		stage.Input, stage.Model = run.Task, run.Model
		stage.ProviderSecret, stage.ProviderVariable = r.ProviderSecret, r.ProviderKey
	}
	observation, err := r.Pods.ObservePilot(ctx, stage, r.Image, r.GitHub.PilotToken)
	if err != nil {
		return db.PilotUpdate{}, err
	}
	switch observation.Phase {
	case "Failed":
		return failure("Pilot " + run.Step + " stage failed. No additional publication was attempted."), nil
	case "Succeeded":
		switch run.Step {
		case "implement":
			return candidate(observation.Output), nil
		case "verify":
			if sha.MatchString(observation.Result) {
				return db.PilotUpdate{Step: "publish", Tree: observation.Result, Evidence: "Local Go tests passed."}, nil
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
	base, patch, ok := strings.Cut(output, "\n")
	if !ok || !sha.MatchString(base) || len(patch) > 240000 || !utf8.ValidString(patch) || strings.ContainsRune(patch, 0) {
		return failure("Implementation returned an invalid patch.")
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
