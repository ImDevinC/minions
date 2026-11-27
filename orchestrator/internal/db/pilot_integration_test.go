package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/imdevinc/minions/orchestrator/internal/db"
)

// TestPilotCheckpointRecovery verifies that clarification and publication resume from saved state.
func TestPilotCheckpointRecovery(t *testing.T) {
	dsn := os.Getenv("MINIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MINIONS_TEST_DATABASE_URL to a disposable database with migrations applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := db.NewMinionStore(pool)
	user := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO users (id, discord_id) VALUES ($1,$2)", user, user.String()); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user) }()
	created, err := store.CreateOrFindDuplicate(ctx, db.CreateMinionParams{Pilot: true, UserID: user,
		Repo: "example/project", Task: "Update docs", Status: db.StatusAwaitingClarification,
		DiscordChannelID: "channel", DiscordMessageID: user.String(),
		ClarificationQuestion: "Which example?"})
	if err != nil {
		t.Fatal(err)
	}
	notReady, _, err := store.AdvancePilot(ctx, nil)
	if err != nil || notReady.ID != uuid.Nil {
		t.Fatalf("awaiting clarification is not runnable: %v, %v", notReady.ID, err)
	}
	id := created.Minion.ID
	if err := store.BindPilotQuestion(ctx, id, "Which example?", "first"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetClarificationAnswer(ctx, db.SetClarificationAnswerParams{ID: id, Answer: "Bypass"}); !errors.Is(err, db.ErrInvalidStatusTransition) {
		t.Fatalf("legacy reply was not rejected: %v", err)
	}
	if err := store.ApplyPilotReply(ctx, db.PilotReply{ID: id, Channel: "channel", Expected: "first", Author: "not-owner", Answer: "Bypass"}); !errors.Is(err, db.ErrInvalidStatusTransition) {
		t.Fatalf("reply by a different user was not rejected: %v", err)
	}
	reply := db.PilotReply{ID: id, Author: user.String(), Channel: "channel", Expected: "first", Answer: "Search",
		Message: "second", Question: "Which syntax?"}
	if err := store.ApplyPilotReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPilotReply(ctx, reply); !errors.Is(err, db.ErrInvalidStatusTransition) {
		t.Fatalf("stale first answer was not rejected: %v", err)
	}
	reply.Expected, reply.Answer, reply.Message, reply.Question = "second", "Go", "", ""
	if err := store.ApplyPilotReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	queued, err := store.ListPending(ctx)
	if err != nil || len(queued) != 0 {
		t.Fatalf("legacy spawner must not see this run: %v, %v", queued, err)
	}
	for _, next := range []struct {
		expected string
		update   db.PilotUpdate
	}{
		{"implement", db.PilotUpdate{Target: "release/default"}},
		{"implement", db.PilotUpdate{Step: "verify", Base: "base", Patch: "patch"}},
		{"verify", db.PilotUpdate{Step: "publish", Tree: "tree"}},
		{"publish", db.PilotUpdate{Head: "head"}},
		{"publish", db.PilotUpdate{Step: "ci", PR: "https://github.com/example/project/pull/1"}},
		{"ci", db.PilotUpdate{Complete: true, Evidence: "Checks passed"}},
	} {
		// A fresh store simulates a new poller taking over the same durable run.
		run, _, err := db.NewMinionStore(pool).AdvancePilot(ctx, func(run db.PilotRun) (db.PilotUpdate, error) {
			if run.ID != id || run.Step != next.expected || run.Task != "Update docs\n\nRequester clarification:\nSearch\n\nGo" {
				t.Fatalf("unexpected checkpoint: %+v", run)
			}
			if next.update.Target == "" && run.Target != "release/default" {
				t.Fatalf("target branch not recovered: %q", run.Target)
			}
			return next.update, nil
		})
		if err != nil || run.ID != id {
			t.Fatalf("advance from %s: %v", next.expected, err)
		}
	}
	finished, err := store.GetByID(ctx, id)
	if err != nil || finished.Status != db.StatusCompleted || finished.PRURL == nil {
		t.Fatalf("unexpected final result: %v, %v", finished, err)
	}
}
