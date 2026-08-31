package repository_test

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestMigration038_ExploreSchema(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	for _, table := range []string{
		"explore_registry_providers", "explore_source_observations", "explore_fetch_runs",
		"explore_fetch_queue", "explore_articles", "explore_batches", "explore_batch_sources",
		"explore_feedback", "explore_article_events",
	} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1 AND relkind = 'r')`, table).Scan(&exists); err != nil {
			t.Fatalf("%s lookup: %v", table, err)
		}
		if !exists {
			t.Errorf("%s was not created", table)
		}
	}

	for _, column := range []string{
		"site_url", "normalized_url", "validation_status", "verified_at", "last_checked_at",
		"last_fetched_at", "etag", "last_modified", "health_score", "last_error",
		"first_discovered_at", "last_observed_at",
	} {
		var exists bool
		if err := db.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				 WHERE table_name = 'recommended_feeds' AND column_name = $1
			)`, column).Scan(&exists); err != nil {
			t.Fatalf("recommended_feeds.%s lookup: %v", column, err)
		}
		if !exists {
			t.Errorf("recommended_feeds.%s was not created", column)
		}
	}

	for _, constraint := range []struct{ table, name string }{
		{"explore_registry_providers", "explore_registry_providers_provider_key_key"},
		{"explore_source_observations", "explore_source_observations_provider_id_fkey"},
		{"explore_fetch_runs", "explore_fetch_runs_provider_id_fkey"},
		{"explore_fetch_queue", "explore_fetch_queue_run_id_fkey"},
		{"explore_articles", "explore_articles_source_observation_id_fkey"},
		{"explore_batches", "explore_batches_user_id_fkey"},
		{"explore_batch_sources", "explore_batch_sources_batch_id_fkey"},
		{"explore_batch_sources", "explore_batch_sources_source_observation_id_fkey"},
		{"explore_feedback", "explore_feedback_user_id_fkey"},
		{"explore_article_events", "explore_article_events_user_id_fkey"},
		{"explore_article_events", "explore_article_events_explore_article_id_fkey"},
	} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = $1::regclass AND conname = $2)`, constraint.table, constraint.name).Scan(&exists); err != nil {
			t.Fatalf("constraint %s.%s lookup: %v", constraint.table, constraint.name, err)
		}
		if !exists {
			t.Errorf("missing FK %s.%s", constraint.table, constraint.name)
		}
	}

	for _, index := range []string{
		"idx_feeds_owner_url", "idx_explore_source_observations_provider_normalized_url",
		"idx_explore_fetch_queue_claimable", "idx_explore_articles_source_published_at",
		"idx_explore_batches_user_created_at", "idx_explore_batch_sources_batch_rank",
		"idx_explore_feedback_user_source_active", "idx_explore_article_events_user_article",
	} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_class WHERE relkind = 'i' AND relname = $1)`, index).Scan(&exists); err != nil {
			t.Fatalf("index %s lookup: %v", index, err)
		}
		if !exists {
			t.Errorf("missing index %s", index)
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

	for _, provider := range []struct{ key, endpoint string }{
		{"plenary-programming-opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Programming.opml"},
		{"plenary-tech-opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Tech.opml"},
		{"plenary-webdev-opml", "https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Web%20Development.opml"},
		{"chinese-independent", "https://raw.githubusercontent.com/timqian/chinese-independent-blogs/master/feed.opml"},
		{"ooh-recently-added", "https://ooh.directory/feeds/recently-added.xml"},
		{"reddit-programming", "/reddit/subreddit/programming"},
		{"awesome-selfhosted", "https://raw.githubusercontent.com/awesome-selfhosted/awesome-selfhosted/master/README.md"},
	} {
		var endpoint string
		if err := db.QueryRow(`SELECT endpoint FROM explore_registry_providers WHERE provider_key = $1`, provider.key).Scan(&endpoint); err != nil {
			t.Errorf("provider %s: %v", provider.key, err)
			continue
		}
		if endpoint != provider.endpoint {
			t.Errorf("provider %s endpoint: got %q, want %q", provider.key, endpoint, provider.endpoint)
		}
	}
}

func TestMigration038_ExploreChecksAndPartialFeedbackIndex(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	if _, err := db.Exec(`INSERT INTO recommended_feeds (url, title, category, language, validation_status) VALUES ('https://invalid-status.test/feed', 'invalid', 'test', 'en', 'unknown')`); err == nil {
		t.Fatal("recommended_feeds accepted an unknown validation status")
	}
	var providerID, runID, sourceID, articleID, userID int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('explore-checks', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_registry_providers (provider_key, endpoint, kind, topic) VALUES ('explore-checks', 'https://example.test/checks', 'opml', 'test') RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_fetch_runs (provider_id, status) VALUES ($1, 'running') RETURNING id`, providerID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_source_observations (provider_id, source_url, normalized_url, title) VALUES ($1, 'https://example.test/source', 'https://example.test/source', 'source') RETURNING id`, providerID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_articles (source_observation_id, title, url, normalized_url) VALUES ($1, 'article', 'https://example.test/article', 'https://example.test/article') RETURNING id`, sourceID).Scan(&articleID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO explore_fetch_queue (run_id, status) VALUES ($1, 'unknown')`, runID); err == nil {
		t.Error("explore_fetch_queue accepted an unknown status")
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, source_observation_id, feedback_type) VALUES ($1, $2, 'unknown')`, userID, sourceID); err == nil {
		t.Error("explore_feedback accepted an unknown feedback type")
	}
	if _, err := db.Exec(`INSERT INTO explore_article_events (user_id, explore_article_id, event_type) VALUES ($1, $2, 'unknown')`, userID, articleID); err == nil {
		t.Error("explore_article_events accepted an unknown event type")
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, source_observation_id, feedback_type) VALUES ($1, $2, 'hide_source')`, userID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_feedback (user_id, source_observation_id, feedback_type) VALUES ($1, $2, 'hide_source')`, userID, sourceID); err == nil {
		t.Fatal("active feedback partial unique index accepted a duplicate")
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

func TestMigration038_ExploreRunClaimedCountCap(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var providerID, runID int
	if err := db.QueryRow(`INSERT INTO explore_registry_providers (provider_key, endpoint, kind, topic) VALUES ('claimed-count-test', 'https://example.test/feed', 'opml', 'test') RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_fetch_runs (provider_id, status, claimed_count) VALUES ($1, 'running', 500) RETURNING id`, providerID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE explore_fetch_runs SET claimed_count = 501 WHERE id = $1`, runID); err == nil {
		t.Fatal("claimed_count=501 accepted")
	}
}
