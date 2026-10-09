package repository_test

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
	"time"
)

func TestRedditBrowserTransactionAndReplay(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch := explore.RedditBrowserBatch{Subreddit: "programming", Period: "week", CapturedAt: now, Listing: json.RawMessage(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"abc","score":100,"subreddit":"programming","url":"https://blog.example/new-post"}}]}}`)}
	repo := repository.NewRedditBrowserRepository(db)
	result, err := repo.Ingest(context.Background(), batch, now)
	if err != nil || result.Accepted != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = repo.Ingest(context.Background(), batch, now)
	if err != nil || !result.Duplicate || result.Accepted != 0 {
		t.Fatalf("replay=%+v err=%v", result, err)
	}
	var observations, tasks int
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE external_key='reddit:abc'`).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue q JOIN recommended_feeds s ON q.source_id=s.id WHERE s.url='https://blog.example/new-post'`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if observations != 1 || tasks != 1 {
		t.Fatalf("observations=%d tasks=%d", observations, tasks)
	}
	// Force a queue failure after candidate upsert: no partial evidence survives.
	if _, err := db.Exec(`ALTER TABLE explore_fetch_queue ADD CONSTRAINT reject_reddit_test CHECK (priority <> 300) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	batch.Period = "month"
	batch.Listing = json.RawMessage(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"def","score":101,"subreddit":"programming","url":"https://blog.example/rollback"}}]}}`)
	if _, err := repo.Ingest(context.Background(), batch, now); err == nil {
		t.Fatal("queue failure ignored")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE url='https://blog.example/rollback'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("partial candidate was committed")
	}
	var success *time.Time
	if err := db.QueryRow(`SELECT last_success_at FROM explore_registry_providers WHERE provider_key='reddit-programming-top-month'`).Scan(&success); err != nil {
		t.Fatal(err)
	}
	if success != nil {
		t.Fatal("failed batch updated provider success")
	}
}
func TestRedditBrowserTransportMigration(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	due, err := repository.NewExploreRegistryRepository(db).LoadDueProviders(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range due {
		if p.Kind == "reddit_top" {
			t.Fatal("worker still fetching browser seed")
		}
	}
	for _, file := range []string{"050_reddit_top_seeds.sql", "052_reddit_browser_transport.sql"} {
		body, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM explore_registry_providers WHERE provider_kind='reddit_top' AND browser_only AND enabled`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("browser seeds=%d", count)
	}
}

func TestRegisterRedditSubreddit(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewRedditBrowserRepository(db)
	ctx := context.Background()
	for _, input := range []string{"GoLang", "golang"} {
		name, err := repo.RegisterSubreddit(ctx, input)
		if err != nil || name != "golang" {
			t.Fatalf("name=%q err=%v", name, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM explore_registry_providers WHERE provider_key LIKE 'reddit-golang-top-%' AND browser_only AND enabled AND sync_interval_minutes=360 AND endpoint LIKE '%#min_score=100'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	now := time.Now().UTC()
	batch := explore.RedditBrowserBatch{Subreddit: "golang", Period: "week", CapturedAt: now, Listing: json.RawMessage(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"newboard","score":100,"subreddit":"golang","url":"https://blog.example/go"}}]}}`)}
	if result, err := repo.Ingest(ctx, batch, now); err != nil || result.Accepted != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	batch.Subreddit = "rust"
	if _, err := repo.Ingest(ctx, batch, now); err != repository.ErrRedditBrowserDisabled {
		t.Fatalf("unregistered err=%v", err)
	}

	if err := repo.RemoveSubreddit(ctx, "golang"); err != nil {
		t.Fatal(err)
	}
	batch.Subreddit = "golang"
	if _, err := repo.Ingest(ctx, batch, now); err != repository.ErrRedditBrowserDisabled {
		t.Fatalf("removed ingestion err=%v", err)
	}
	items, err := repo.ListSubreddits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Name == "golang" {
			found = true
			if item.Enabled {
				t.Fatal("removed still enabled")
			}
		}
	}
	if !found {
		t.Fatal("removed seed/history lost")
	}
	if _, err := repo.RegisterSubreddit(ctx, "golang"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_registry_providers WHERE provider_key LIKE 'reddit-golang-top-%' AND enabled`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restore count=%d err=%v", count, err)
	}
	// Incompatible providers must still roll back both windows.
	if _, err := db.Exec(`DELETE FROM explore_registry_providers WHERE provider_key='reddit-golang-top-week';UPDATE explore_registry_providers SET browser_only=false WHERE provider_key='reddit-golang-top-month'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterSubreddit(ctx, "golang"); err != repository.ErrRedditBrowserDisabled {
		t.Fatalf("incompatible err=%v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_registry_providers WHERE provider_key='reddit-golang-top-week'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial count=%d err=%v", count, err)
	}
}
