package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PilotReply contains a reply and, if necessary, its replacement question.
type PilotReply struct {
	ID                                                   uuid.UUID
	Author, Channel, Expected, Answer, Question, Message string
}

// BindPilotQuestion attaches the Discord delivery to a saved first question.
func (s *MinionStore) BindPilotQuestion(ctx context.Context, id uuid.UUID, question, message string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE minions SET clarification_message_id=$3
		WHERE id=$1 AND status='awaiting_clarification' AND clarification_question=$2
		AND (clarification_message_id IS NULL OR clarification_message_id=$3)
		AND EXISTS (SELECT 1 FROM minion_pilot_runs WHERE minion_id=$1 AND clarification_round=1)`, id, question, message)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrInvalidStatusTransition
	}
	return err
}

// ApplyPilotReply records an answer only for the active question, channel, and requester.
func (s *MinionStore) ApplyPilotReply(ctx context.Context, reply PilotReply) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var round int
	err = tx.QueryRow(ctx, `SELECT p.clarification_round
		FROM minions m JOIN minion_pilot_runs p ON p.minion_id=m.id JOIN users u ON u.id=m.user_id
		WHERE m.id=$1 AND m.status='awaiting_clarification' AND m.clarification_message_id=$2
		AND m.discord_channel_id=$3 AND u.discord_id=$4 AND m.created_at > now()-interval '24 hours'
		FOR UPDATE OF m,p`, reply.ID, reply.Expected, reply.Channel, reply.Author).Scan(&round)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidStatusTransition
	}
	if err != nil {
		return err
	}
	status := StatusPending
	if reply.Question != "" {
		if round != 1 || reply.Message == "" {
			return ErrInvalidStatusTransition
		}
		status = StatusAwaitingClarification
		if _, err := tx.Exec(ctx, `UPDATE minion_pilot_runs SET clarification_round=2 WHERE minion_id=$1`, reply.ID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE minions SET status=$2,
		clarification_answer=concat_ws(E'\n\n', clarification_answer, $3::text),
		clarification_question=COALESCE(NULLIF($4,''),clarification_question),
		clarification_message_id=COALESCE(NULLIF($5,''),clarification_message_id),last_activity_at=now()
		WHERE id=$1`, reply.ID, status, reply.Answer, reply.Question, reply.Message)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PilotRound returns zero for a run outside the pilot.
func (s *MinionStore) PilotRound(ctx context.Context, id uuid.UUID) (int, error) {
	var round int
	err := s.pool.QueryRow(ctx, `SELECT clarification_round FROM minion_pilot_runs WHERE minion_id=$1`, id).Scan(&round)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return round, err
}
