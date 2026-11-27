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

// TestDiscordDeliveryReplay verifies that replays bypass quota without merging distinct requests.
func TestDiscordDeliveryReplay(t *testing.T) {
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

	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, discord_id) VALUES ($1, $2)`, userID, userID.String()); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID) }()

	store := db.NewMinionStore(pool)
	req := db.CreateMinionParams{
		DiscordMessageID: uuid.NewString(),
		Platform:         db.PlatformDiscord,
		Repo:             "ImDevinC/go-fifa",
		Task:             "Update the README",
		UserID:           userID,
	}
	first, err := store.CreateOrFindDuplicate(ctx, req)
	if err != nil || first.WasDuplicate {
		t.Fatalf("create first run: %v, %v", first, err)
	}
	for range db.MaxConcurrentMinions - 1 {
		next := req
		next.DiscordMessageID = uuid.NewString()
		distinct, err := store.CreateOrFindDuplicate(ctx, next)
		if err != nil || distinct.WasDuplicate || distinct.Minion.ID == first.Minion.ID {
			t.Fatalf("create a distinct run: %v, %v", distinct, err)
		}
	}
	replayed, err := store.CreateOrFindDuplicate(ctx, req)
	if err != nil || !replayed.WasDuplicate || replayed.Minion.ID != first.Minion.ID {
		t.Fatalf("replay at quota: %v, %v", replayed, err)
	}
	req.DiscordMessageID = uuid.NewString()
	excess, err := store.CreateOrFindDuplicate(ctx, req)
	if !errors.Is(err, db.ErrConcurrentLimitExceeded) || excess != nil {
		t.Fatalf("expected quota error for a new message: %v, %v", excess, err)
	}
}
