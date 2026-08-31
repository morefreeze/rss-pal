package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestClipExploreErrorPreservesUTF8WithinByteLimit(t *testing.T) {
	short := "短错误🙂"
	if got := clipExploreError(errors.New(short)); got != short {
		t.Fatalf("short error clipped: got %q want %q", got, short)
	}

	// Seven bytes per repetition means a raw 1000-byte slice cuts an emoji.
	input := strings.Repeat("中🙂", 400)
	got := clipExploreError(errors.New(input))
	if len(got) > 1000 {
		t.Fatalf("clip length=%d, want <=1000", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("clip returned invalid UTF-8: %q", got[len(got)-8:])
	}
}

func TestExploreQueueClaimRunAdvisoryLockBusyDoesNotMutate(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var held bool
	if err := tx.QueryRow(`SELECT pg_try_advisory_xact_lock($1)`, exploreDispatcherAdvisoryLock).Scan(&held); err != nil || !held {
		t.Fatalf("hold advisory lock held=%t err=%v", held, err)
	}

	repo := NewExploreQueueRepository(db)
	run, tasks, err := repo.ClaimRun(time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC), "blocked", time.Now().Add(time.Hour), 500)
	if !errors.Is(err, ErrExploreDispatcherBusy) || run != nil || tasks != nil {
		t.Fatalf("ClaimRun while lock held run=%+v tasks=%+v err=%v", run, tasks, err)
	}
	var runs, leases int
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE status = 'leased'`).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || leases != 0 {
		t.Fatalf("busy claim mutated state runs=%d leases=%d", runs, leases)
	}
}

func TestExploreQueueClaimRunClosedDatabaseReturnsErrorNotBusy(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://postgres:postgres@127.0.0.1:1/rsspal_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = NewExploreQueueRepository(db).ClaimRun(time.Now(), "closed", time.Now().Add(time.Hour), 1)
	if err == nil || errors.Is(err, ErrExploreDispatcherBusy) {
		t.Fatalf("closed DB error=%v, want non-busy query error", err)
	}
}
