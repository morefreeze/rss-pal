package repository_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRelatedSeedSQLFairlyBoundsEachVisibleOwnerBeforeGlobalLimit(t *testing.T) {
	normalized := strings.Join(strings.Fields(repository.ExploreRelatedSeedsSQL), " ")
	for _, fragment := range []string{
		"SELECT owner_key,url,seed_at FROM raw_seeds",
		"ORDER BY owner_key,seed_at DESC,url",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("related seed SQL missing %q: %s", fragment, normalized)
		}
	}
	for _, forbidden := range []string{"ROW_NUMBER()", "owner_rank", "LIMIT $2"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("related seed SQL caps before full canonicalization via %q: %s", forbidden, normalized)
		}
	}
}

func TestSelectExploreRelatedSeedsCanonicalDedupDoesNotConsumeOwnerQuota(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	raw := []repository.ExploreRelatedSeed{
		{OwnerKey: 7, URL: "HTTPS://Example.COM/feed?utm_source=a#top", SeedAt: now},
		{OwnerKey: 7, URL: "https://example.com/feed", SeedAt: now.Add(-time.Minute)},
		{OwnerKey: 7, URL: "https://example.com/second", SeedAt: now.Add(-2 * time.Minute)},
		{OwnerKey: 9, URL: "https://other.example/feed", SeedAt: now},
	}
	got := repository.SelectExploreRelatedSeeds(raw, now, 3)
	if len(got) != 3 {
		t.Fatalf("seeds=%v, canonical duplicate consumed quota", got)
	}
	counts := map[string]int{}
	for _, seed := range got {
		counts[seed]++
	}
	if counts["https://example.com/feed"] != 1 || counts["https://example.com/second"] != 1 || counts["https://other.example/feed"] != 1 {
		t.Fatalf("canonical seeds=%v", got)
	}
}

func TestSelectExploreRelatedSeedsCanonicalizesElevenTrackingVariantsBeforeOwnerQuota(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	raw := make([]repository.ExploreRelatedSeed, 0, 12)
	for index := 0; index < 11; index++ {
		raw = append(raw, repository.ExploreRelatedSeed{
			OwnerKey: 7,
			URL:      fmt.Sprintf("https://example.com/feed?utm_source=variant-%02d#fragment", index),
			SeedAt:   now.Add(-time.Duration(index) * time.Minute),
		})
	}
	raw = append(raw, repository.ExploreRelatedSeed{OwnerKey: 7, URL: "https://example.com/unique", SeedAt: now.Add(-12 * time.Minute)})
	got := repository.SelectExploreRelatedSeeds(raw, now, 10)
	if len(got) != 2 || got[0] != "https://example.com/feed" || got[1] != "https://example.com/unique" {
		t.Fatalf("seeds=%v, older unique URL was starved by tracking variants", got)
	}
}

func TestSelectExploreRelatedSeedsRotatesMoreThan250OwnersWithBoundedWait(t *testing.T) {
	window := 6 * time.Hour
	start := time.Unix(0, 0).UTC().Add(10_000 * window)
	raw := make([]repository.ExploreRelatedSeed, 0, 251)
	for owner := 1; owner <= 251; owner++ {
		raw = append(raw, repository.ExploreRelatedSeed{OwnerKey: owner, URL: fmt.Sprintf("https://owner-%03d.example/feed", owner), SeedAt: start})
	}
	seen := map[string]struct{}{}
	for cycle := 0; cycle < 2; cycle++ {
		got := repository.SelectExploreRelatedSeeds(raw, start.Add(time.Duration(cycle)*window), 200)
		if len(got) != 200 {
			t.Fatalf("cycle %d seeds=%d want=200", cycle, len(got))
		}
		for _, seed := range got {
			seen[seed] = struct{}{}
		}
	}
	if len(seen) != 251 {
		t.Fatalf("owners observed in two cycles=%d want=251", len(seen))
	}
}

func TestExploreRegistryRelatedSeedsRotateAcrossMoreThan250Owners(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	if _, err := db.Exec(`
		INSERT INTO users(username,password_hash)
		SELECT 'related-owner-' || value,'x' FROM generate_series(1,251) value`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO feeds(url,title,owner_id,last_fetched_at)
		SELECT 'https://related-owner-' || row_number() OVER (ORDER BY id) || '.example/feed',
		       'owner seed',id,$1
		FROM users WHERE username LIKE 'related-owner-%'`, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewExploreRegistryRepository(db)
	start := time.Unix(0, 0).UTC().Add(10_000 * 6 * time.Hour)
	seen := map[string]struct{}{}
	for cycle := 0; cycle < 2; cycle++ {
		seeds, err := repo.LoadRelatedSeeds(context.Background(), start.Add(time.Duration(cycle)*6*time.Hour), 200)
		if err != nil {
			t.Fatal(err)
		}
		if len(seeds) != 200 {
			t.Fatalf("cycle %d seeds=%d want=200", cycle, len(seeds))
		}
		for _, seed := range seeds {
			seen[seed] = struct{}{}
		}
	}
	if len(seen) != 251 {
		t.Fatalf("database owners observed in two cycles=%d want=251", len(seen))
	}
}

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
