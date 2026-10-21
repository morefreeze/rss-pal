package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestSummaryLeaseExcludesOtherOwnersAndRejectsExpiredResult(t *testing.T) {
	repo, db, _, article := seedLinkSetParent(t)
	token, err := repo.ClaimSummary(context.Background(), article, true)
	if err != nil || token == "" {
		t.Fatalf("claim %q %v", token, err)
	}
	if _, err := repo.ClaimSummary(context.Background(), article, true); !errors.Is(err, ErrSummaryInProgress) {
		t.Fatalf("duplicate %v", err)
	}
	if _, err := db.Exec(`UPDATE article_summary_jobs SET lease_expires_at=now()-interval '1 second' WHERE article_id=$1`, article); err != nil {
		t.Fatal(err)
	}
	replacement, err := repo.ClaimSummary(context.Background(), article, true)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err := db.QueryRow(`SELECT content FROM articles WHERE id=$1`, article).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteSummary(article, token, body, "old", "old"); err != nil || ok {
		t.Fatalf("stale %v %v", ok, err)
	}
	if err := repo.FailSummary(article, token, errors.New("old owner's late failure")); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetSummaryJob(context.Background(), article)
	if err != nil || current.State != "running" {
		t.Fatalf("late failure poisoned successor: %+v %v", current, err)
	}
	if ok, err := repo.CompleteSummary(article, replacement, body, "new", "new"); err != nil || !ok {
		t.Fatalf("current %v %v", ok, err)
	}
	job, err := repo.GetSummaryJob(context.Background(), article)
	if err != nil || job.State != "done" {
		t.Fatalf("job %+v %v", job, err)
	}
}
func TestSummaryFailureBackoffAllowsManualRetry(t *testing.T) {
	repo, _, _, article := seedLinkSetParent(t)
	token, err := repo.ClaimSummary(context.Background(), article, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FailSummary(article, token, errors.New("provider unavailable")); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetSummaryJob(context.Background(), article)
	if err != nil || job.RetryAt == nil || !job.RetryAt.After(time.Now()) {
		t.Fatalf("job %+v %v", job, err)
	}
	if _, err := repo.ClaimSummary(context.Background(), article, false); !errors.Is(err, ErrSummaryInProgress) {
		t.Fatal(err)
	}
	if _, err := repo.ClaimSummary(context.Background(), article, true); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryBackfillSkipsFailedNotDueAndRunningJobs(t *testing.T) {
	repo, db, feed, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET content=repeat('article body ',20) WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var older int
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,fetched_at) VALUES($1,'Older','https://summary.example/older',repeat('article body ',20),now()-interval '1 day') RETURNING id`, feed).Scan(&older); err != nil {
		t.Fatal(err)
	}
	token, err := repo.ClaimSummary(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	articles, err := repo.GetArticlesWithoutSummary(1)
	if err != nil || len(articles) != 1 || articles[0].ID != older {
		t.Fatalf("running selection %+v %v", articles, err)
	}
	if err := repo.FailSummary(id, token, errors.New("failure")); err != nil {
		t.Fatal(err)
	}
	articles, err = repo.GetArticlesWithoutSummary(1)
	if err != nil || len(articles) != 1 || articles[0].ID != older {
		t.Fatalf("failed selection %+v %v", articles, err)
	}
	if _, err := db.Exec(`UPDATE article_summary_jobs SET retry_at=now()-interval '1 second' WHERE article_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	articles, err = repo.GetArticlesWithoutSummary(1)
	if err != nil || len(articles) != 1 || articles[0].ID != id {
		t.Fatalf("due selection %+v %v", articles, err)
	}
}

func TestSummaryLeaseRLSCompletionUsesAuthorizedRequestTransaction(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	var owner, other, feed, id int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('summary-rls-owner','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('summary-rls-other','x') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,owner_id) VALUES('https://summary-rls.example',$1) RETURNING id`, owner).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,url,title,content) VALUES($1,'https://summary-rls.example/post','Title','body') RETURNING id`, feed).Scan(&id); err != nil {
		t.Fatal(err)
	}
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	repo := NewArticleRepository(app)
	repo.SetSummaryJobDB(db)
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []int{other, owner} {
		tx, err := app.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`SELECT set_config('app.user_id',$1,true)`, fmt.Sprint(user)); err != nil {
			t.Fatal(err)
		}
		bound := repo.WithCtx(fakeCtx{ctxkey.Tx: tx})
		ok, err := bound.CompleteSummary(id, token, "body", "brief", "detailed")
		if err != nil {
			t.Fatal(err)
		}
		if ok != (user == owner) {
			t.Fatalf("user %d saved=%v", user, ok)
		}
		if user == owner {
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		} else {
			_ = tx.Rollback()
		}
	}
}

func TestSummaryFailureHonorsBudgetReset(t *testing.T) {
	repo, _, _, id := seedLinkSetParent(t)
	token, err := repo.ClaimSummary(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(3 * time.Hour)
	if err := repo.FailSummary(id, token, &taskbudget.LimitError{Reason: "user_daily", Bucket: "ai", RetryAt: &reset}); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetSummaryJob(context.Background(), id)
	if err != nil || job.RetryAt == nil || job.RetryAt.Before(reset.Add(-time.Second)) {
		t.Fatalf("budget reset lost: %+v %v", job, err)
	}
}

func TestSummaryLeaseRejectsChangedContentWithoutReplacingPriorSummary(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET content='original',summary_brief='prior brief',summary_detailed='prior detailed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE articles SET content='updated' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteSummary(id, token, "original", "stale brief", "stale detailed"); err != nil || ok {
		t.Fatalf("stale content accepted: %v %v", ok, err)
	}
	var summary string
	if err := db.QueryRow(`SELECT summary_brief FROM articles WHERE id=$1`, id).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "prior brief" {
		t.Fatal(summary)
	}
}

func TestSummaryJobReadRecoversExpiredRegenerationWithPriorSummaries(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET summary_brief='prior brief',summary_detailed='prior detailed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE article_summary_jobs SET lease_expires_at=now()-interval '1 second' WHERE article_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetSummaryJob(context.Background(), id)
	if err != nil || job.State != "failed" || job.LastError == "" || job.RetryAt == nil {
		t.Fatalf("expired regeneration still running: %+v %v", job, err)
	}
	if replacement, err := repo.ClaimSummary(context.Background(), id, true); err != nil || replacement == token {
		t.Fatalf("retry %q %v", replacement, err)
	}
}
func TestSummaryFailureDoesNotExposeProviderDetails(t *testing.T) {
	repo, _, _, id := seedLinkSetParent(t)
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FailSummary(id, token, errors.New("Post https://internal.example/?api_key=secret: dial tcp 10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetSummaryJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(job.LastError, "secret") || strings.Contains(job.LastError, "internal") || job.LastError == "" {
		t.Fatalf("provider details leaked: %+v", job)
	}
}
