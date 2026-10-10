package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreLeaseTokensAreUniqueOpaqueCredentials(t *testing.T) {
	first, err := newExploreLeaseToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newExploreLeaseToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || len(second) != 64 || first == second {
		t.Fatalf("lease tokens malformed or reused lengths=%d/%d equal=%t", len(first), len(second), first == second)
	}
	payload, err := json.Marshal(model.ExploreFetchTask{LeaseToken: &first})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), first) || strings.Contains(string(payload), "lease_token") {
		t.Fatalf("lease token leaked through task JSON: %s", payload)
	}
}

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

func TestExploreQueueSQLKeepsFreshClaimsSeparateFromOriginalRunRecovery(t *testing.T) {
	source, err := os.ReadFile("explore_queue.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"status = 'pending' AND run_id IS NULL AND not_before <= CURRENT_TIMESTAMP",
		"ORDER BY run.window_at ASC, run.id ASC",
		"lease_token",
		"SET lease_owner = $2, lease_token = $3, lease_expires_at",
		"SET status = 'leased', run_id = $2, lease_owner = $3, lease_token = $4",
		"WHERE id = $1 AND run_id = $2 AND status = 'leased' AND lease_token = $3",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("queue SQL missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"OR (status = 'leased' AND lease_expires_at <= CURRENT_TIMESTAMP)",
		"status = 'leased' AND lease_owner = $3 AND lease_expires_at > CURRENT_TIMESTAMP",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("fresh claim SQL still rewrites expired original-run work: %q", forbidden)
		}
	}
}

func TestExploreQueueWithQuerierPreservesRawDatabase(t *testing.T) {
	raw := &sql.DB{}
	repo := NewExploreQueueRepository(raw)
	tx := (*sql.Tx)(nil)
	bound := repo.WithQuerier(tx)
	if bound == repo {
		t.Fatal("WithQuerier returned the mutable original repository")
	}
	if bound.db != tx || bound.rawDB != raw {
		t.Fatalf("bound db=%T raw=%p, want *sql.Tx and raw=%p", bound.db, bound.rawDB, raw)
	}
	if repo.db != raw || repo.rawDB != raw {
		t.Fatal("WithQuerier mutated the original repository")
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
	run, tasks, err := repo.ClaimRun(time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC), "blocked", time.Hour, 500)
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

func TestExploreQueueClaimRunAdvisoryQueryErrorRollsBack(t *testing.T) {
	checkDB, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	if _, err := checkDB.Exec(`
		CREATE FUNCTION pg_try_advisory_xact_lock(bigint) RETURNS boolean
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced advisory error'; END $$
	`); err != nil {
		t.Fatal(err)
	}

	shadowDB := openExploreShadowDB(t, schema)
	defer shadowDB.Close()
	shadowDB.SetMaxOpenConns(1)
	shadowDB.SetMaxIdleConns(1)
	if err := shadowDB.Ping(); err != nil {
		t.Fatal(err)
	}

	run, tasks, err := NewExploreQueueRepository(shadowDB).ClaimRun(time.Now(), "forced-error", time.Hour, 1)
	if err == nil || errors.Is(err, ErrExploreDispatcherBusy) || run != nil || tasks != nil {
		t.Fatalf("forced advisory query run=%+v tasks=%+v err=%v", run, tasks, err)
	}
	var runs, leases int
	if err := checkDB.QueryRow(`SELECT count(*) FROM explore_fetch_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := checkDB.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE status = 'leased'`).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || leases != 0 {
		t.Fatalf("advisory query error mutated state runs=%d leases=%d", runs, leases)
	}
}

func openExploreShadowDB(t *testing.T, schema string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",pg_catalog")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRelatedRediscoveryDoesNotResetProgress(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var provider int
	if err := db.QueryRow(`SELECT id FROM explore_registry_providers WHERE provider_key='related-sites'`).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := db.QueryRow(`INSERT INTO explore_related_tasks(provider_id,canonical_seed_url,attempts,updated_at) VALUES($1,'https://progress.example/',1,now()-interval '2 hours') RETURNING updated_at`, provider).Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = enqueueRelatedSeed(context.Background(), tx, provider, "https://progress.example/", 200); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err = tx.QueryRow(`SELECT updated_at FROM explore_related_tasks WHERE canonical_seed_url='https://progress.example/'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Fatalf("rediscovery fabricated progress: %v -> %v", before, after)
	}
}
