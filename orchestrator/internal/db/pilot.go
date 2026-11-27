package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PilotRun is a checkpoint for the version-one go-fifa workflow.
type PilotRun struct {
	Base, Head, Patch, Step, Task, Model, Tree string
	Channel                                    *string
	ID                                         uuid.UUID
	Status                                     MinionStatus
	ChangedAt                                  time.Time
}

// PilotUpdate describes the next checkpoint or a terminal outcome.
type PilotUpdate struct {
	Base, Head, Patch, Step, Tree, PR, Failure, Evidence string
	Delay                                                time.Duration
	Complete                                             bool
}

// PilotProgress returns the current stage and last saved verification evidence.
func (s *MinionStore) PilotProgress(ctx context.Context, id uuid.UUID) (string, string, error) {
	var step, evidence string
	err := s.pool.QueryRow(ctx, `SELECT step,evidence FROM minion_pilot_runs WHERE minion_id=$1`, id).Scan(&step, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return step, evidence, err
}

// NextPilotCleanup selects a terminal run whose resources may still exist.
func (s *MinionStore) NextPilotCleanup(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT p.minion_id FROM minion_pilot_runs p JOIN minions m ON p.minion_id=m.id
		WHERE NOT p.cleaned AND (p.step='done' OR m.status='failed')
		ORDER BY p.changed_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// MarkPilotCleaned records that all pods and ConfigMaps have been removed.
func (s *MinionStore) MarkPilotCleaned(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE minion_pilot_runs SET cleaned=true WHERE minion_id=$1`, id)
	return err
}

// AdvancePilot locks one eligible run while its worker observes or starts a bounded operation.
func (s *MinionStore) AdvancePilot(ctx context.Context, advance func(PilotRun) (PilotUpdate, error)) (PilotRun, PilotUpdate, error) {
	var run PilotRun
	var update PilotUpdate
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return run, update, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		SELECT m.id,m.status,m.model,m.task || COALESCE(E'\n\nRequester clarification:\n' || m.clarification_answer,''),
			m.discord_channel_id,p.step,p.base_sha,p.head_sha,p.patch,p.tree_sha,p.changed_at
		FROM minion_pilot_runs p JOIN minions m ON m.id=p.minion_id
		WHERE p.step <> 'done' AND p.next_poll <= now()
			AND m.status IN ('pending','running','terminated')
		ORDER BY p.next_poll,m.id LIMIT 1 FOR UPDATE OF m,p SKIP LOCKED
	`).Scan(&run.ID, &run.Status, &run.Model, &run.Task, &run.Channel, &run.Step, &run.Base, &run.Head, &run.Patch, &run.Tree, &run.ChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, update, nil
	}
	if err != nil {
		return run, update, err
	}
	update, err = advance(run)
	if err != nil {
		return run, update, err
	}
	if update.Step == "" {
		update.Step = run.Step
	}
	if update.Complete {
		update.Step = "done"
	}
	_, err = tx.Exec(ctx, `UPDATE minion_pilot_runs
		SET step=$2, next_poll=now()+make_interval(secs=>$3),
			changed_at=CASE WHEN step <> $2 OR $9 THEN now() ELSE changed_at END,
			base_sha=COALESCE(NULLIF($4,''),base_sha), head_sha=COALESCE(NULLIF($5,''),head_sha),
			patch=COALESCE(NULLIF($6,''),patch), tree_sha=COALESCE(NULLIF($7,''),tree_sha),
			evidence=COALESCE(NULLIF($8,''),evidence)
		WHERE minion_id=$1`, run.ID, update.Step, update.Delay.Seconds(), update.Base, update.Head, update.Patch, update.Tree, update.Evidence, run.Status == StatusPending)
	if err != nil {
		return run, update, err
	}
	status := StatusRunning
	if run.Status == StatusTerminated {
		status = StatusTerminated
	} else if update.Failure != "" {
		status = StatusFailed
	} else if update.Complete {
		status = StatusCompleted
	}
	_, err = tx.Exec(ctx, `UPDATE minions SET status=$2, started_at=COALESCE(started_at,now()),
		completed_at=CASE WHEN $3 THEN COALESCE(completed_at,now()) ELSE completed_at END,
		pr_url=COALESCE(NULLIF($4,''),pr_url), error=NULLIF($5,''), last_activity_at=now()
		WHERE id=$1`, run.ID, status, update.Complete, update.PR, update.Failure)
	if err != nil {
		return run, update, err
	}
	return run, update, tx.Commit(ctx)
}
