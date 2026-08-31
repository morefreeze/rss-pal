package repository_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestMigration038_ExploreSchema(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	for table, want := range map[string][]string{
		"explore_registry_providers":  {"id", "provider_key", "provider_kind", "endpoint", "topic", "sync_interval_minutes", "enabled", "etag", "last_modified", "last_sync_at", "last_success_at", "consecutive_failures", "last_error", "created_at", "updated_at"},
		"explore_source_observations": {"id", "provider_id", "source_id", "external_key", "provider_tags", "first_seen_at", "last_seen_at", "occurrence_count"},
		"explore_fetch_runs":          {"id", "window_at", "status", "claimed_count", "started_at", "completed_at", "worker_id", "error_message", "created_at"},
		"explore_fetch_queue":         {"id", "source_id", "task_type", "status", "priority", "not_before", "attempts", "run_id", "lease_owner", "lease_expires_at", "last_error", "created_at", "updated_at", "completed_at"},
		"explore_articles":            {"id", "source_id", "url", "normalized_url", "title", "content", "excerpt", "published_at", "fetched_at", "created_at", "updated_at"},
		"explore_batches":             {"id", "user_id", "slot_at", "status", "source_count", "error_message", "created_at", "completed_at"},
		"explore_batch_sources":       {"id", "user_id", "batch_id", "source_id", "rank", "score", "topic", "reason"},
		"explore_feedback":            {"id", "user_id", "source_id", "topic", "feedback_type", "created_at"},
		"explore_article_events":      {"id", "user_id", "explore_article_id", "event_type", "occurred_at"},
	} {
		assertExactColumns(t, db, table, want)
	}

	for _, column := range []string{"site_url", "normalized_url", "validation_status", "verified_at", "last_checked_at", "last_fetched_at", "etag", "last_modified", "health_score", "last_error", "first_discovered_at", "last_observed_at"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'recommended_feeds' AND column_name = $1)`, column).Scan(&exists); err != nil {
			t.Fatalf("recommended_feeds.%s lookup: %v", column, err)
		}
		if !exists {
			t.Errorf("recommended_feeds.%s was not created", column)
		}
	}

	for _, constraint := range []struct{ table, name, target string }{
		{"explore_source_observations", "explore_source_observations_provider_id_fkey", "explore_registry_providers"},
		{"explore_source_observations", "explore_source_observations_source_id_fkey", "recommended_feeds"},
		{"explore_fetch_queue", "explore_fetch_queue_source_id_fkey", "recommended_feeds"},
		{"explore_fetch_queue", "explore_fetch_queue_run_id_fkey", "explore_fetch_runs"},
		{"explore_articles", "explore_articles_source_id_fkey", "recommended_feeds"},
		{"explore_batches", "explore_batches_user_id_fkey", "users"},
		{"explore_batch_sources", "explore_batch_sources_user_id_fkey", "users"},
		{"explore_batch_sources", "explore_batch_sources_batch_id_user_id_fkey", "explore_batches"},
		{"explore_batch_sources", "explore_batch_sources_source_id_fkey", "recommended_feeds"},
		{"explore_feedback", "explore_feedback_user_id_fkey", "users"},
		{"explore_feedback", "explore_feedback_source_id_fkey", "recommended_feeds"},
		{"explore_article_events", "explore_article_events_user_id_fkey", "users"},
		{"explore_article_events", "explore_article_events_explore_article_id_fkey", "explore_articles"},
	} {
		var target string
		if err := db.QueryRow(`SELECT ccu.table_name FROM information_schema.table_constraints tc JOIN information_schema.constraint_column_usage ccu ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema WHERE tc.table_name = $1 AND tc.constraint_name = $2 AND tc.constraint_type = 'FOREIGN KEY'`, constraint.table, constraint.name).Scan(&target); err != nil {
			t.Errorf("FK %s.%s: %v", constraint.table, constraint.name, err)
		} else if target != constraint.target {
			t.Errorf("FK %s.%s target: got %s, want %s", constraint.table, constraint.name, target, constraint.target)
		}
	}

	for _, table := range []string{"explore_batches", "explore_batch_sources", "explore_feedback", "explore_article_events"} {
		var enabled, forced bool
		if err := db.QueryRow(`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, table).Scan(&enabled, &forced); err != nil {
			t.Fatalf("RLS flags %s: %v", table, err)
		}
		if !enabled || !forced {
			t.Errorf("%s RLS flags: enabled=%t forced=%t", table, enabled, forced)
		}
	}
	var batchSourcePolicy string
	if err := db.QueryRow(`SELECT pg_get_expr(polqual, polrelid) FROM pg_policy WHERE polrelid = 'explore_batch_sources'::regclass AND polname = 'explore_batch_sources_user_isolation'`).Scan(&batchSourcePolicy); err != nil {
		t.Fatalf("explore_batch_sources policy: %v", err)
	}
	if !strings.Contains(batchSourcePolicy, "user_id") || strings.Contains(batchSourcePolicy, "explore_batches") {
		t.Errorf("explore_batch_sources must use direct user_id RLS policy, got %q", batchSourcePolicy)
	}
	var batchesOwnerKey bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'explore_batches'::regclass AND conname = 'explore_batches_id_user_id_key')`).Scan(&batchesOwnerKey); err != nil {
		t.Fatalf("explore_batches owner key: %v", err)
	}
	if !batchesOwnerKey {
		t.Error("explore_batches must expose UNIQUE(id, user_id) for the composite child FK")
	}
	var legacyBatchOnlyFK bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'explore_batch_sources'::regclass AND conname = 'explore_batch_sources_batch_id_fkey')`).Scan(&legacyBatchOnlyFK); err != nil {
		t.Fatalf("legacy batch FK lookup: %v", err)
	}
	if legacyBatchOnlyFK {
		t.Error("explore_batch_sources retains bypassable batch_id-only FK")
	}

	for _, provider := range exploreProviderSeeds {
		var kind, endpoint string
		var topic *string
		var interval int
		if err := db.QueryRow(`SELECT provider_kind, endpoint, topic, sync_interval_minutes FROM explore_registry_providers WHERE provider_key = $1`, provider.key).Scan(&kind, &endpoint, &topic, &interval); err != nil {
			t.Errorf("provider %s: %v", provider.key, err)
			continue
		}
		if kind != provider.kind || endpoint != provider.endpoint || interval != provider.interval || topic == nil || *topic != provider.topic {
			t.Errorf("provider %s: got kind=%q endpoint=%q topic=%v interval=%d", provider.key, kind, endpoint, topic, interval)
		}
	}
}

func TestMigration038_ExploreChecksAndUniqueIndexes(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	if _, err := db.Exec(`INSERT INTO recommended_feeds (url, title, category, language, normalized_url, health_score) VALUES ('https://health-low.test/feed', 'low', 'test', 'en', 'https://health-low.test/feed', -0.01)`); err == nil {
		t.Fatal("recommended_feeds accepted health_score < 0")
	}
	if _, err := db.Exec(`INSERT INTO recommended_feeds (url, title, category, language, normalized_url, health_score) VALUES ('https://health-high.test/feed', 'high', 'test', 'en', 'https://health-high.test/feed', 1.01)`); err == nil {
		t.Fatal("recommended_feeds accepted health_score > 1")
	}
	var sourceID, userID int
	if err := db.QueryRow(`INSERT INTO recommended_feeds (url, title, category, language, normalized_url, health_score) VALUES ('https://source.test/feed', 'source', 'test', 'en', 'https://source.test/feed', 0.5) RETURNING id`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO recommended_feeds (url, title, category, language, normalized_url) VALUES ('https://source-alias.test/feed', 'duplicate', 'test', 'en', 'https://source.test/feed')`); err == nil {
		t.Fatal("recommended_feeds accepted duplicate normalized_url")
	}
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('explore-checks', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var providerID, articleID, batchID int
	if err := db.QueryRow(`INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, sync_interval_minutes) VALUES ('explore-checks', 'opml', 'https://example.test/checks', 360) RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, sync_interval_minutes) VALUES ('invalid-kind', 'invalid', 'https://example.test/invalid', 360)`); err == nil {
		t.Fatal("provider accepted an unknown provider_kind")
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations (provider_id, source_id, external_key) VALUES ($1, $2, 'checks')`, providerID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations (provider_id, source_id, external_key) VALUES ($1, $2, 'checks')`, providerID, sourceID); err == nil {
		t.Fatal("source observation accepted duplicate provider/external/source identity")
	}
	if err := db.QueryRow(`INSERT INTO explore_articles (source_id, url, normalized_url, title) VALUES ($1, 'https://source.test/article', 'https://source.test/article', 'article') RETURNING id`, sourceID).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_articles (source_id, url, normalized_url, title) VALUES ($1, 'https://source.test/article-alias', 'https://source.test/article', 'duplicate')`, sourceID); err == nil {
		t.Fatal("explore articles accepted duplicate source/normalized URL")
	}
	if err := db.QueryRow(`INSERT INTO explore_batches (user_id, slot_at, status) VALUES ($1, NOW(), 'pending') RETURNING id`, userID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, query string
		args        []any
	}{
		{"run status", `INSERT INTO explore_fetch_runs (window_at, status) VALUES (NOW(), 'pending')`, nil},
		{"queue task", `INSERT INTO explore_fetch_queue (source_id, task_type) VALUES ($1, 'unknown')`, []any{sourceID}},
		{"queue status", `INSERT INTO explore_fetch_queue (source_id, task_type, status) VALUES ($1, 'validate_source', 'unknown')`, []any{sourceID}},
		{"batch status", `INSERT INTO explore_batches (user_id, slot_at, status) VALUES ($1, NOW() + interval '1 minute', 'running')`, []any{userID}},
		{"feedback type", `INSERT INTO explore_feedback (user_id, source_id, feedback_type) VALUES ($1, $2, 'unknown')`, []any{userID, sourceID}},
		{"feedback hide shape", `INSERT INTO explore_feedback (user_id, topic, feedback_type) VALUES ($1, 'go', 'hide_source')`, []any{userID}},
		{"feedback topic shape", `INSERT INTO explore_feedback (user_id, source_id, feedback_type) VALUES ($1, $2, 'dampen_topic')`, []any{userID, sourceID}},
		{"event type", `INSERT INTO explore_article_events (user_id, explore_article_id, event_type) VALUES ($1, $2, 'read')`, []any{userID, articleID}},
	} {
		if _, err := db.Exec(tc.query, tc.args...); err == nil {
			t.Errorf("%s accepted an invalid value", tc.name)
		}
	}

	window := "2026-08-31 08:00:00"
	if _, err := db.Exec(`INSERT INTO explore_fetch_runs (window_at, status, claimed_count) VALUES ($1, 'running', 500)`, window); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_fetch_runs (window_at, status) VALUES ($1, 'done')`, window); err == nil {
		t.Fatal("global run accepted duplicate window_at")
	}
	if _, err := db.Exec(`INSERT INTO explore_fetch_runs (window_at, status, claimed_count) VALUES ($1, 'running', 501)`, "2026-08-31 11:00:00"); err == nil {
		t.Fatal("claimed_count=501 accepted")
	}
	if _, err := db.Exec(`INSERT INTO explore_fetch_queue (source_id, task_type, status) VALUES ($1, 'validate_source', 'pending')`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_fetch_queue (source_id, task_type, status) VALUES ($1, 'validate_source', 'leased')`, sourceID); err == nil {
		t.Fatal("queue partial unique index accepted pending/leased duplicate")
	}
	if _, err := db.Exec(`INSERT INTO explore_batches (user_id, slot_at, status) VALUES ($1, (SELECT slot_at FROM explore_batches WHERE id = $2), 'done')`, userID, batchID); err == nil {
		t.Fatal("batch unique index accepted duplicate user/slot")
	}
	if _, err := db.Exec(`INSERT INTO explore_batch_sources (user_id, batch_id, source_id, rank, score) VALUES ($1, $2, $3, 1, 1)`, userID, batchID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_batch_sources (user_id, batch_id, source_id, rank, score) VALUES ($1, $2, $3, 2, 2)`, userID, batchID, sourceID); err == nil {
		t.Fatal("batch sources accepted duplicate batch/source")
	}
	var otherUserID, otherBatchID int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('explore-other-user', 'x') RETURNING id`).Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_batches (user_id, slot_at, status) VALUES ($1, NOW(), 'pending') RETURNING id`, otherUserID).Scan(&otherBatchID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_batch_sources (user_id, batch_id, source_id, rank, score) VALUES ($1, $2, $3, 1, 1)`, userID, otherBatchID, sourceID); err == nil {
		t.Fatal("batch source accepted a user different from its batch owner")
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, source_id, feedback_type) VALUES ($1, $2, 'hide_source')`, userID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, source_id, feedback_type) VALUES ($1, $2, 'hide_source')`, userID, sourceID); err == nil {
		t.Fatal("feedback unique index accepted duplicate active feedback")
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, topic, feedback_type) VALUES ($1, 'go', 'dampen_topic')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, topic, feedback_type) VALUES ($1, 'go', 'dampen_topic')`, userID); err == nil {
		t.Fatal("feedback topic unique index accepted a duplicate")
	}
}

func TestMigration038_ProviderSeedConflictPreservesRuntimeState(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	seed := exploreProviderSeeds[0]
	if _, err := db.Exec(`UPDATE explore_registry_providers SET enabled = false, etag = 'keep-etag', last_modified = 'keep-last-modified', last_sync_at = NOW(), last_success_at = NOW(), consecutive_failures = 7, last_error = 'keep-error' WHERE provider_key = $1`, seed.key); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "038_subscription_explore.sql"))
	if err != nil {
		t.Fatalf("read real migration: %v", err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatalf("re-run real migration: %v", err)
	}
	var enabled bool
	var kind, endpoint string
	var topic *string
	var interval int
	var etag, lastModified, lastError string
	var failures int
	var lastSync, lastSuccess time.Time
	if err := db.QueryRow(`SELECT enabled, provider_kind, endpoint, topic, sync_interval_minutes, etag, last_modified, last_sync_at, last_success_at, consecutive_failures, last_error FROM explore_registry_providers WHERE provider_key = $1`, seed.key).Scan(&enabled, &kind, &endpoint, &topic, &interval, &etag, &lastModified, &lastSync, &lastSuccess, &failures, &lastError); err != nil {
		t.Fatal(err)
	}
	if kind != seed.kind || endpoint != seed.endpoint || topic == nil || *topic != seed.topic || interval != seed.interval {
		t.Fatalf("real migration did not restore seed values: kind=%q endpoint=%q topic=%v interval=%d", kind, endpoint, topic, interval)
	}
	if enabled || etag != "keep-etag" || lastModified != "keep-last-modified" || lastSync.IsZero() || lastSuccess.IsZero() || failures != 7 || lastError != "keep-error" {
		t.Fatalf("real migration overwrote runtime state: enabled=%t etag=%q last_modified=%q last_sync=%v last_success=%v failures=%d last_error=%q", enabled, etag, lastModified, lastSync, lastSuccess, failures, lastError)
	}
}

func TestMigration038_FeedURLIsOwnerScoped(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userA, userB int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('explore-owner-a', 'x') RETURNING id`).Scan(&userA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('explore-owner-b', 'x') RETURNING id`).Scan(&userB); err != nil {
		t.Fatal(err)
	}
	for _, ownerID := range []any{userA, userB, nil} {
		if _, err := db.Exec(`INSERT INTO feeds (url, title, owner_id) VALUES ('https://same.example/feed', 'same', $1)`, ownerID); err != nil {
			t.Fatalf("insert owner %v: %v", ownerID, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO feeds (url, title, owner_id) VALUES ('https://same.example/feed', 'duplicate', $1)`, userA); err == nil {
		t.Fatal("same owner accepted duplicate URL")
	}
	if _, err := db.Exec(`INSERT INTO feeds (url, title) VALUES ('https://same.example/feed', 'duplicate shared')`); err == nil {
		t.Fatal("shared feeds accepted duplicate URL")
	}
}

type providerSeed struct {
	key, kind, endpoint, topic string
	interval                   int
}

var exploreProviderSeeds = []providerSeed{
	{"plenary-programming-opml", "opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Programming.opml", "programming", 360},
	{"plenary-tech-opml", "opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Tech.opml", "technology", 360},
	{"plenary-webdev-opml", "opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Web%20Development.opml", "web-development", 360},
	{"chinese-independent", "opml", "https://raw.githubusercontent.com/timqian/chinese-independent-blogs/master/feed.opml", "chinese-independent", 360},
	{"ooh-recently-added", "directory", "https://ooh.directory/feeds/recently-added.xml", "recently-added", 360},
	{"reddit-programming", "reddit_stream", "/reddit/subreddit/programming", "programming", 360},
	{"awesome-selfhosted", "github_awesome", "https://raw.githubusercontent.com/awesome-selfhosted/awesome-selfhosted/master/README.md", "self-hosted", 360},
}

func assertExactColumns(t *testing.T, db interface {
	Query(string, ...any) (*sql.Rows, error)
}, table string, want []string) {
	t.Helper()
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_name = $1`, table)
	if err != nil {
		t.Fatalf("columns %s: %v", table, err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column %s: %v", table, err)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("columns %s: got %v, want %v", table, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns %s: got %v, want %v", table, got, want)
		}
	}
}
