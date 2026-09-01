package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestProductionRelatedDiscoveryQueuesSeedsBeforeNetwork(t *testing.T) {
	source, err := os.ReadFile("explore_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "NewExploreRelatedTaskRepository") || !strings.Contains(text, "NewExploreRelatedTaskProcessor") {
		t.Fatal("production related discovery is not split into durable producer and leased processor")
	}
	for _, forbidden := range []string{"RelatedSiteSync{", "LoadRelatedSeeds("} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("production runtime still performs legacy related sync via %q", forbidden)
		}
	}
}

func TestExploreSchedulerQueueClaimBoundaryPrefers500HealthyThenBroken(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	var providerID int
	if err := db.QueryRow(`SELECT id FROM explore_registry_providers WHERE provider_kind<>'related_site' ORDER BY id LIMIT 1`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		WITH inserted AS (
			INSERT INTO recommended_feeds(url,title,category,language,normalized_url,validation_status,is_broken,health_score,last_checked_at,last_fetched_at)
			SELECT 'https://healthy-boundary-'||n||'.example/feed','healthy','test','en','https://healthy-boundary-'||n||'.example/feed','valid',false,1,$1-INTERVAL '4 hours',$1-INTERVAL '4 hours'
			FROM generate_series(1,500) n RETURNING id,normalized_url
		)
		INSERT INTO explore_source_observations(provider_id,source_id,external_key,last_seen_at)
		SELECT $2,id,normalized_url,$1 FROM inserted`, now, providerID); err != nil {
		t.Fatal(err)
	}
	var brokenID int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,category,language,normalized_url,validation_status,is_broken,health_score,last_checked_at,last_fetched_at) VALUES ('https://broken-boundary.example/feed','broken','test','en','https://broken-boundary.example/feed','valid',true,0,$1-INTERVAL '25 hours',$1-INTERVAL '25 hours') RETURNING id`, now).Scan(&brokenID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations(provider_id,source_id,external_key,last_seen_at) VALUES ($1,$2,'broken-boundary',$3)`, providerID, brokenID, now.Add(-26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	queue := repository.NewExploreQueueRepository(db)
	scheduler := &scheduledExploreRegistry{registry: &fakeExploreRegistry{}, catalog: repository.NewExploreCatalogRepository(db), queue: queue}
	if _, err := scheduler.SyncDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	run, tasks, err := queue.ClaimRun(now, "boundary", time.Hour, 500)
	if err != nil || len(tasks) != 500 || run.ClaimedCount != 500 {
		t.Fatalf("first run=%+v tasks=%d err=%v", run, len(tasks), err)
	}
	for _, task := range tasks {
		if task.SourceID == brokenID {
			t.Fatal("broken health check displaced healthy refresh")
		}
	}
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET status='done',completed_at=$1,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL WHERE run_id=$2`, now, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET last_checked_at=$1,last_fetched_at=$1 WHERE id<>$2`, now, brokenID); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.SyncDue(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, remaining, err := queue.ClaimRun(now.Add(time.Minute), "boundary", time.Hour, 500)
	if err != nil || len(remaining) != 1 || second.ClaimedCount != 1 {
		t.Fatalf("second run=%+v tasks=%+v err=%v", second, remaining, err)
	}
	if remaining[0].SourceID != brokenID || remaining[0].Priority != repository.ExplorePriorityBrokenHealthCheck {
		t.Fatalf("broken task=%+v", remaining[0])
	}
}
