package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRepoTaskHash(t *testing.T) {
	// Same inputs should produce same hash
	hash1 := repoTaskHash("owner/repo", "fix the bug")
	hash2 := repoTaskHash("owner/repo", "fix the bug")
	if hash1 != hash2 {
		t.Error("same inputs should produce same hash")
	}

	// Different repos should produce different hashes
	hash3 := repoTaskHash("owner/other-repo", "fix the bug")
	if hash1 == hash3 {
		t.Error("different repos should produce different hashes")
	}

	// Different tasks should produce different hashes
	hash4 := repoTaskHash("owner/repo", "different task")
	if hash1 == hash4 {
		t.Error("different tasks should produce different hashes")
	}

	// Empty inputs should work and be distinct
	hashEmpty := repoTaskHash("", "")
	hashRepoOnly := repoTaskHash("owner/repo", "")
	hashTaskOnly := repoTaskHash("", "some task")

	if hashEmpty == hashRepoOnly {
		t.Error("empty repo vs repo-only should be different")
	}
	if hashEmpty == hashTaskOnly {
		t.Error("empty vs task-only should be different")
	}
	if hashRepoOnly == hashTaskOnly {
		t.Error("repo-only vs task-only should be different")
	}

	// Verify separator prevents concatenation collisions
	// "owner/repo" + "\0" + "task" should differ from "owner/repo\0ta" + "\0" + "sk"
	hashA := repoTaskHash("owner/repo", "task")
	hashB := repoTaskHash("owner/repo\x00ta", "sk")
	if hashA == hashB {
		t.Error("separator should prevent concatenation collisions")
	}
}

func TestDuplicateWindowConstant(t *testing.T) {
	// Verify the duplicate window is 5 minutes as specified
	if DuplicateWindow.Minutes() != 5 {
		t.Errorf("DuplicateWindow should be 5 minutes, got %v", DuplicateWindow)
	}
}

type mockPool struct {
	execResults map[string]execResult
}

type execResult struct {
	rowsAffected int64
	err          error
}

func (m *mockPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, nil
}

func (m *mockPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func (m *mockPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	// Find matching query in mock results
	if result, ok := m.execResults[sql]; ok {
		return pgconn.NewCommandTag(string(rune(result.rowsAffected))), result.err
	}
	return pgconn.NewCommandTag(""), nil
}
