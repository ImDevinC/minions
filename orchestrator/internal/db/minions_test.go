package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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

// mockScanner implements Scanner for testing scanMinion
type mockScanner struct {
	values []any
	err    error
}

func (m *mockScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	if len(dest) != len(m.values) {
		panic("mockScanner: dest and values length mismatch")
	}
	for i, v := range m.values {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = v.(uuid.UUID)
		case *string:
			*d = v.(string)
		case *MinionStatus:
			*d = v.(MinionStatus)
		case *Platform:
			*d = v.(Platform)
		case **string:
			if v == nil {
				*d = nil
			} else {
				s := v.(string)
				*d = &s
			}
		case *int64:
			*d = v.(int64)
		case *float64:
			*d = v.(float64)
		case *time.Time:
			*d = v.(time.Time)
		case **time.Time:
			if v == nil {
				*d = nil
			} else {
				t := v.(time.Time)
				*d = &t
			}
		default:
			panic("mockScanner: unsupported type")
		}
	}
	return nil
}

func TestScanMinion(t *testing.T) {
	// Create test data for all 32 fields
	testID := uuid.New()
	testUserID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)
	clarificationQ := "What version?"
	prURL := "https://github.com/org/repo/pull/1"
	podName := "minion-abc123"
	sessionID := "sess-xyz"
	channelID := "123456789"
	messageID := "987654321"
	matrixEventID := "$event123"
	matrixRoomID := "!room:matrix.org"
	branch := "feat/my-feature"
	sourcePRURL := "https://github.com/org/repo/pull/42"

	scanner := &mockScanner{
		values: []any{
			testID,              // ID
			testUserID,          // UserID
			"owner/repo",        // Repo
			"fix the bug",       // Task
			"claude-3-5-sonnet", // Model
			StatusRunning,       // Status
			PlatformDiscord,     // Platform
			clarificationQ,      // ClarificationQuestion (*string)
			nil,                 // ClarificationAnswer (*string)
			nil,                 // ClarificationMessageID (*string)
			nil,                 // MatrixClarificationEventID (*string)
			int64(1000),         // InputTokens
			int64(500),          // OutputTokens
			int64(200),          // ReasoningTokens
			int64(5000),         // CacheReadTokens
			int64(100),          // CacheWriteTokens
			float64(0.05),       // CostUSD
			prURL,               // PRURL (*string)
			nil,                 // Error (*string)
			sessionID,           // SessionID (*string)
			podName,             // PodName (*string)
			messageID,           // DiscordMessageID (*string)
			channelID,           // DiscordChannelID (*string)
			matrixEventID,       // MatrixEventID (*string)
			matrixRoomID,        // MatrixRoomID (*string)
			branch,              // Branch (*string)
			sourcePRURL,         // SourcePRURL (*string)
			nil,                 // GitHubCommentID (*string)
			now,                 // CreatedAt
			now,                 // StartedAt (*time.Time)
			nil,                 // CompletedAt (*time.Time)
			now,                 // LastActivityAt
		},
	}

	m, err := scanMinion(scanner)
	if err != nil {
		t.Fatalf("scanMinion failed: %v", err)
	}

	// Verify all 29 fields
	if m.ID != testID {
		t.Errorf("ID: got %v, want %v", m.ID, testID)
	}
	if m.UserID != testUserID {
		t.Errorf("UserID: got %v, want %v", m.UserID, testUserID)
	}
	if m.Repo != "owner/repo" {
		t.Errorf("Repo: got %v, want %v", m.Repo, "owner/repo")
	}
	if m.Task != "fix the bug" {
		t.Errorf("Task: got %v, want %v", m.Task, "fix the bug")
	}
	if m.Model != "claude-3-5-sonnet" {
		t.Errorf("Model: got %v, want %v", m.Model, "claude-3-5-sonnet")
	}
	if m.Status != StatusRunning {
		t.Errorf("Status: got %v, want %v", m.Status, StatusRunning)
	}
	if m.Platform != PlatformDiscord {
		t.Errorf("Platform: got %v, want %v", m.Platform, PlatformDiscord)
	}
	if m.ClarificationQuestion == nil || *m.ClarificationQuestion != clarificationQ {
		t.Errorf("ClarificationQuestion: got %v, want %v", m.ClarificationQuestion, &clarificationQ)
	}
	if m.ClarificationAnswer != nil {
		t.Errorf("ClarificationAnswer: got %v, want nil", m.ClarificationAnswer)
	}
	if m.ClarificationMessageID != nil {
		t.Errorf("ClarificationMessageID: got %v, want nil", m.ClarificationMessageID)
	}
	if m.MatrixClarificationEventID != nil {
		t.Errorf("MatrixClarificationEventID: got %v, want nil", m.MatrixClarificationEventID)
	}
	if m.InputTokens != 1000 {
		t.Errorf("InputTokens: got %v, want %v", m.InputTokens, 1000)
	}
	if m.OutputTokens != 500 {
		t.Errorf("OutputTokens: got %v, want %v", m.OutputTokens, 500)
	}
	if m.ReasoningTokens != 200 {
		t.Errorf("ReasoningTokens: got %v, want %v", m.ReasoningTokens, 200)
	}
	if m.CacheReadTokens != 5000 {
		t.Errorf("CacheReadTokens: got %v, want %v", m.CacheReadTokens, 5000)
	}
	if m.CacheWriteTokens != 100 {
		t.Errorf("CacheWriteTokens: got %v, want %v", m.CacheWriteTokens, 100)
	}
	if m.CostUSD != 0.05 {
		t.Errorf("CostUSD: got %v, want %v", m.CostUSD, 0.05)
	}
	if m.PRURL == nil || *m.PRURL != prURL {
		t.Errorf("PRURL: got %v, want %v", m.PRURL, &prURL)
	}
	if m.Error != nil {
		t.Errorf("Error: got %v, want nil", m.Error)
	}
	if m.SessionID == nil || *m.SessionID != sessionID {
		t.Errorf("SessionID: got %v, want %v", m.SessionID, &sessionID)
	}
	if m.PodName == nil || *m.PodName != podName {
		t.Errorf("PodName: got %v, want %v", m.PodName, &podName)
	}
	if m.DiscordMessageID == nil || *m.DiscordMessageID != messageID {
		t.Errorf("DiscordMessageID: got %v, want %v", m.DiscordMessageID, &messageID)
	}
	if m.DiscordChannelID == nil || *m.DiscordChannelID != channelID {
		t.Errorf("DiscordChannelID: got %v, want %v", m.DiscordChannelID, &channelID)
	}
	if m.MatrixEventID == nil || *m.MatrixEventID != matrixEventID {
		t.Errorf("MatrixEventID: got %v, want %v", m.MatrixEventID, &matrixEventID)
	}
	if m.MatrixRoomID == nil || *m.MatrixRoomID != matrixRoomID {
		t.Errorf("MatrixRoomID: got %v, want %v", m.MatrixRoomID, &matrixRoomID)
	}
	if m.Branch == nil || *m.Branch != branch {
		t.Errorf("Branch: got %v, want %v", m.Branch, &branch)
	}
	if m.SourcePRURL == nil || *m.SourcePRURL != sourcePRURL {
		t.Errorf("SourcePRURL: got %v, want %v", m.SourcePRURL, &sourcePRURL)
	}
	if m.GitHubCommentID != nil {
		t.Errorf("GitHubCommentID: got %v, want nil", m.GitHubCommentID)
	}
	if !m.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", m.CreatedAt, now)
	}
	if m.StartedAt == nil || !m.StartedAt.Equal(now) {
		t.Errorf("StartedAt: got %v, want %v", m.StartedAt, &now)
	}
	if m.CompletedAt != nil {
		t.Errorf("CompletedAt: got %v, want nil", m.CompletedAt)
	}
	if !m.LastActivityAt.Equal(now) {
		t.Errorf("LastActivityAt: got %v, want %v", m.LastActivityAt, now)
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
