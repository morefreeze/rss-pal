package repository_test

import (
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreRegistryUpsertPreservesValidSourceAndObservation(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var providerID int
	if err := db.QueryRow(`INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, topic) VALUES ('test-opml','opml','https://registry.example/opml','go') RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewExploreRegistryRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	sourceID, err := repo.UpsertCandidate(providerID, explore.Candidate{ExternalKey: "example", FeedURL: "https://example.com/feed", SiteURL: "https://example.com", Title: "Example", Topic: "go", Tags: []string{"go"}, OccurrenceCount: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET validation_status='valid' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertCandidate(providerID, explore.Candidate{ExternalKey: "example", FeedURL: "https://example.com/feed", Title: "Example Updated", Topic: "go", Tags: []string{"go", "rss"}, OccurrenceCount: 3}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var status, title string
	if err := db.QueryRow(`SELECT validation_status,title FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &title); err != nil {
		t.Fatal(err)
	}
	if status != "valid" || title != "Example Updated" {
		t.Fatalf("source status=%q title=%q", status, title)
	}
	var occurrences int
	if err := db.QueryRow(`SELECT occurrence_count FROM explore_source_observations WHERE provider_id=$1 AND source_id=$2 AND external_key='example'`, providerID, sourceID).Scan(&occurrences); err != nil {
		t.Fatal(err)
	}
	if occurrences != 3 {
		t.Fatalf("occurrence_count=%d", occurrences)
	}
}

func TestExploreRegistryDueBackoffAndSuccessState(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, sync_interval_minutes) VALUES ('due-opml','opml','https://registry.example/opml',60)`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewExploreRegistryRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	due, err := repo.LoadDueProviders(now)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due=%#v err=%v", due, err)
	}
	if err := repo.RecordFailure(due[0].ID, now, assertErr("broken")); err != nil {
		t.Fatal(err)
	}
	due, err = repo.LoadDueProviders(now.Add(time.Hour))
	if err != nil || len(due) != 0 {
		t.Fatalf("backoff due=%#v err=%v", due, err)
	}
	due, err = repo.LoadDueProviders(now.Add(2*time.Hour + time.Second))
	if err != nil || len(due) != 1 {
		t.Fatalf("due after backoff=%#v err=%v", due, err)
	}
	if err := repo.RecordSuccess(due[0].ID, now.Add(2*time.Hour), `"etag"`, "Wed"); err != nil {
		t.Fatal(err)
	}
	var failures int
	var lastError *string
	if err := db.QueryRow(`SELECT consecutive_failures,last_error FROM explore_registry_providers WHERE id=$1`, due[0].ID).Scan(&failures, &lastError); err != nil {
		t.Fatal(err)
	}
	if failures != 0 || lastError != nil {
		t.Fatalf("success state failures=%d error=%v", failures, lastError)
	}
}

func TestExploreRegistryQueueAdapterEnqueuesValidation(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	sourceID := insertExploreSource(t, db, 1)
	queue := repository.NewExploreRegistryQueue(repository.NewExploreQueueRepository(db))
	if err := queue.Enqueue(sourceID, "validate_source", 300); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND task_type='validate_source'`, sourceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("queued=%d", count)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
