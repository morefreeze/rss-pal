package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

type captureQueueFetcher struct {
	urls   []string
	owners []int
	err    error
	cancel context.CancelFunc
}

func TestQueuedCaptureBudgetDenialDoesNotStarveOtherOwners(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	oldStore, oldPolicies := workerBudgets, workerPolicies
	defer func() { workerBudgets, workerPolicies = oldStore, oldPolicies }()
	workerBudgets = taskbudget.New(db)
	workerPolicies = taskbudget.Policies{"background_fetch": {Daily: 1, GlobalDaily: 100, Concurrent: 1, GlobalConcurrent: 10, Lease: time.Minute}}
	var ownerA, ownerB int
	for name, id := range map[string]*int{"budget-a": &ownerA, "budget-b": &ownerB} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewArticleRepository(db)
	for i := 0; i < 10; i++ {
		if _, err := repo.EnqueueCapture(ownerA, fmt.Sprintf("https://example.test/a/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := repo.EnqueueCapture(ownerB, "https://example.test/b")
	if err != nil {
		t.Fatal(err)
	}
	release, err := workerBudgets.Acquire(context.Background(), ownerA, "background_fetch", 1, workerPolicies["background_fetch"])
	if err != nil {
		t.Fatal(err)
	}
	release()
	runner := &captureQueueRunner{}
	f := &captureQueueFetcher{}
	runner.processQueuedCaptures(context.Background(), repo, f)
	if len(f.urls) != 0 {
		t.Fatalf("denied owner was fetched: %v", f.urls)
	}
	runner.processQueuedCaptures(context.Background(), repo, f)
	var state string
	if err := db.QueryRow(`SELECT processing_state FROM articles WHERE id=$1`, b.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || len(f.owners) != 1 || f.owners[0] != ownerB {
		t.Fatalf("other owner starved: state=%s owners=%v", state, f.owners)
	}
	var consumed int
	if err := db.QueryRow(`SELECT sum(refetch_attempts) FROM articles WHERE feed_id IN (SELECT id FROM feeds WHERE owner_id=$1)`, ownerA).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 0 {
		t.Fatal("budget denial consumed fetch attempts")
	}
	if _, err := db.Exec(`UPDATE task_budget_daily SET used=0 WHERE owner_id=$1 AND bucket='background_fetch'`, ownerA); err != nil {
		t.Fatal(err)
	}
	runner.processQueuedCaptures(context.Background(), repo, f)
	if len(f.owners) != 2 || f.owners[1] != ownerA {
		t.Fatalf("cursor did not wrap: owners=%v", f.owners)
	}
}

func (f *captureQueueFetcher) FetchContentWithMetadata(ctx context.Context, pageURL string) (rss.ContentResult, error) {
	f.urls = append(f.urls, pageURL)
	f.owners = append(f.owners, taskbudget.Owner(ctx))
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Minute {
		panic("unbounded capture")
	}
	if f.cancel != nil {
		f.cancel()
		return rss.ContentResult{}, context.Canceled
	}
	published := time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC)
	return rss.ContentResult{Content: "Only this shared article's body", Title: "Shared article", PublishedAt: &published}, f.err
}

func TestQueuedCapturesFetchOnlySharedArticle(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('queued-owner','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	feed, err := repository.NewFeedRepository(db).GetOrCreateClipFeed(owner)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewArticleRepository(db)
	a, err := repo.EnqueueCapture(owner, "https://example.test/shared")
	if err != nil {
		t.Fatal(err)
	}
	// A PDF OCR stub must stay in its own queue.
	pdf := &model.Article{FeedID: feed.ID, URL: "https://example.test/scan.pdf", Title: "Scanned PDF"}
	if err := repo.CreatePDFStub(pdf); err != nil {
		t.Fatal(err)
	}
	runner := &captureQueueRunner{}
	f := &captureQueueFetcher{}
	runner.processQueuedCaptures(context.Background(), repo, f)
	var state, body, title string
	var published time.Time
	var children int
	if err := db.QueryRow(`SELECT processing_state,content,title,published_at FROM articles WHERE id=$1`, a.ID).Scan(&state, &body, &title, &published); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, a.ID).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || body != "Only this shared article's body" || title != "Shared article" || published.Year() != 2026 || children != 0 || len(f.urls) != 1 || f.urls[0] != a.URL || f.owners[0] != owner {
		t.Fatalf("state=%s body=%q title=%q published=%s children=%d urls=%v owners=%v", state, body, title, published, children, f.urls, f.owners)
	}
	ocr, err := repo.GetPDFOCRPending(10)
	if err != nil || len(ocr) != 1 || ocr[0].ID != pdf.ID {
		t.Fatalf("OCR queue=%v error=%v", ocr, err)
	}
	runner.processQueuedCaptures(context.Background(), repo, f)
	if len(f.urls) != 1 {
		t.Fatal("completed article fetched again")
	}
}

func TestQueuedCaptureFailureRetryAndCancellation(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('queued-failure','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewArticleRepository(db)
	a, err := repo.EnqueueCapture(owner, "https://example.test/failure")
	if err != nil {
		t.Fatal(err)
	}
	runner := &captureQueueRunner{}
	f := &captureQueueFetcher{err: errors.New("publisher unavailable")}
	for attempt := 1; attempt <= 3; attempt++ {
		runner.processQueuedCaptures(context.Background(), repo, f)
		var state string
		var attempts int
		if err := db.QueryRow(`SELECT processing_state,refetch_attempts FROM articles WHERE id=$1`, a.ID).Scan(&state, &attempts); err != nil {
			t.Fatal(err)
		}
		want := "queued"
		if attempt == 3 {
			want = "failed"
		}
		if state != want || attempts != attempt {
			t.Fatalf("attempt=%d state=%s attempts=%d", attempt, state, attempts)
		}
	}
	runner.processQueuedCaptures(context.Background(), repo, f)
	if len(f.urls) != 3 {
		t.Fatal("failed article kept retrying")
	}
	if _, err := repo.EnqueueCapture(owner, a.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	runner.processQueuedCaptures(ctx, repo, f)
	var state string
	var attempts int
	if err := db.QueryRow(`SELECT processing_state,refetch_attempts FROM articles WHERE id=$1`, a.ID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || attempts != 0 {
		t.Fatalf("cancellation consumed attempt: %s %d", state, attempts)
	}
}
