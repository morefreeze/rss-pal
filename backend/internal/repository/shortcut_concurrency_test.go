package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

// Wait for the exact backend to reach its conflicting row lock, rather than
// assume that a goroutine has reached the desired interleaving after a sleep.
func waitShortcutRowLock(t *testing.T, db *sql.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shortcut operation never reached the expected row-lock wait")
}

func TestShortcutFirstCaptureDoesNotDeadlockWithRevoke(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('shortcut-first-capture','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := NewShortcutRepository(db)
	code, _, err := repo.Pair(uid)
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Rollback()
	if owner, err := ResolveShortcutOwner(capture, token); err != nil || owner != uid {
		t.Fatalf("resolve owner=%d err=%v", owner, err)
	}
	revoke, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke.Rollback()
	var revokePID int
	if err := revoke.QueryRow(`SELECT pg_backend_pid()`).Scan(&revokePID); err != nil {
		t.Fatal(err)
	}
	revoked := make(chan error, 1)
	go func() {
		err := (&ShortcutRepository{db: revoke}).Revoke(uid)
		if err == nil {
			err = revoke.Commit()
		}
		revoked <- err
	}()
	waitShortcutRowLock(t, db, revokePID)
	// This is the first saved clip. Its owner foreign key needs a user KEY
	// SHARE lock, which must remain compatible with capture's existing locks.
	if _, err := NewFeedRepository(db).WithQuerier(capture).GetOrCreateClipFeed(uid); err != nil {
		t.Fatalf("first clip creation deadlocked with revocation: %v", err)
	}
	if err := capture.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatalf("revocation after capture: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("revocation did not finish after capture committed")
	}
	check, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Rollback()
	if _, err := ResolveShortcutOwner(check, token); !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("credential survived completed revocation: %v", err)
	}
}

func TestShortcutCodeExpiringWhileExchangeWaitsIsRejected(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('shortcut-wait-expiry','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := NewShortcutRepository(db)
	code, _, err := repo.Pair(uid)
	if err != nil {
		t.Fatal(err)
	}
	var expires time.Time
	if err := db.QueryRow(`UPDATE shortcut_pairings SET expires_at=clock_timestamp()+interval '2 seconds' WHERE user_id=$1 RETURNING expires_at`, uid).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if err := lockShortcutUser(blocker, uid); err != nil {
		t.Fatal(err)
	}
	exchange, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer exchange.Rollback()
	var exchangePID int
	if err := exchange.QueryRow(`SELECT pg_backend_pid()`).Scan(&exchangePID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := (&ShortcutRepository{db: exchange}).Connect(code)
		result <- err
	}()
	waitShortcutRowLock(t, db, exchangePID)
	// Cross the actual database expiry while the exchange transaction is
	// blocked. Transaction-start now() must not extend the code's lifetime.
	if _, err := db.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.05)`, expires); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrShortcutConnectionInvalid) {
			t.Fatalf("expired code accepted after waiting for user lock: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("exchange did not finish after user lock released")
	}
}
