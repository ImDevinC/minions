package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

func TestMarkRunning_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to lock the row
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusPending))

	// Expect UPDATE to set running status
	mock.ExpectExec(`UPDATE minions SET`).
		WithArgs(StatusRunning, podName, minionID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Expect commit
	mock.ExpectCommit()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if err != nil {
		t.Errorf("MarkRunning returned error: %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to return no rows
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnError(pgx.ErrNoRows)

	// Expect rollback (deferred)
	mock.ExpectRollback()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_InvalidStatusTransition_FromRunning(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to return running status (not pending)
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusRunning))

	// Expect rollback (deferred)
	mock.ExpectRollback()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got: %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_InvalidStatusTransition_FromCompleted(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to return completed status
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusCompleted))

	// Expect rollback (deferred)
	mock.ExpectRollback()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_InvalidStatusTransition_FromFailed(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to return failed status
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusFailed))

	// Expect rollback (deferred)
	mock.ExpectRollback()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_InvalidStatusTransition_FromAwaitingClarification(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// Expect transaction to begin
	mock.ExpectBegin()

	// Expect SELECT FOR UPDATE to return awaiting_clarification status
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusAwaitingClarification))

	// Expect rollback (deferred)
	mock.ExpectRollback()

	// Execute
	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Errorf("expected ErrInvalidStatusTransition, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_UsesTransaction(t *testing.T) {
	// This test verifies that MarkRunning uses a transaction with FOR UPDATE
	// to prevent concurrent updates. The mock expectations enforce the order:
	// 1. Begin transaction
	// 2. SELECT ... FOR UPDATE (row lock)
	// 3. UPDATE
	// 4. Commit

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	// The order of expectations is critical - it verifies the transaction pattern
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusPending))
	mock.ExpectExec(`UPDATE minions SET`).
		WithArgs(StatusRunning, podName, minionID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err = store.MarkRunning(ctx, minionID, podName)
	if err != nil {
		t.Errorf("MarkRunning returned error: %v", err)
	}

	// ExpectationsWereMet verifies that all expectations were called in order
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("transaction expectations not met: %v", err)
	}
}

func TestMarkRunning_BeginError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	beginErr := errors.New("connection error")
	mock.ExpectBegin().WillReturnError(beginErr)

	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, beginErr) {
		t.Errorf("expected begin error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	queryErr := errors.New("database connection lost")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnError(queryErr)
	mock.ExpectRollback()

	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, queryErr) {
		t.Errorf("expected query error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_UpdateError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	updateErr := errors.New("update failed")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusPending))
	mock.ExpectExec(`UPDATE minions SET`).
		WithArgs(StatusRunning, podName, minionID).
		WillReturnError(updateErr)
	mock.ExpectRollback()

	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, updateErr) {
		t.Errorf("expected update error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkRunning_CommitError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewMinionStoreWithPool(mock)
	ctx := context.Background()
	minionID := uuid.New()
	podName := "minion-abc123"

	commitErr := errors.New("commit failed")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM minions WHERE id = \$1 FOR UPDATE`).
		WithArgs(minionID).
		WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow(StatusPending))
	mock.ExpectExec(`UPDATE minions SET`).
		WithArgs(StatusRunning, podName, minionID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit().WillReturnError(commitErr)
	mock.ExpectRollback()

	err = store.MarkRunning(ctx, minionID, podName)
	if !errors.Is(err, commitErr) {
		t.Errorf("expected commit error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
