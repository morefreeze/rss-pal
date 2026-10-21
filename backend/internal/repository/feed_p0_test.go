package repository

import (
	"context"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"strings"
	"testing"
	"time"
)

func TestFeedRetryStateAndRecovery(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	r := NewFeedRepository(db)
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('retry-owner','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	feed := model.Feed{URL: "https://example.org/rss", OwnerID: &uid, FetchIntervalMin: 60}
	if err := r.Create(&feed); err != nil {
		t.Fatal(err)
	}
	checked := time.Now().UTC().Add(-time.Hour)
	if err := r.UpdateFetchInfo(feed.ID, "etag", "modified", checked); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := r.RecordFetchFailure(feed.ID, "upstream unavailable"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.GetByID(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FetchFailures != 3 || got.FetchError == "" || got.NextAttemptAt == nil || got.LastAttemptAt == nil || got.NextAttemptAt.Sub(*got.LastAttemptAt) < 19*time.Minute || got.ETag != "etag" || got.LastFetchedAt.Unix() != checked.Unix() {
		t.Fatalf("retry state: %+v", got)
	}
	visible, err := r.GetVisibleByUser(uid)
	if err != nil || len(visible) != 1 || visible[0].FetchFailures != 3 {
		t.Fatalf("visible state=%+v err=%v", visible, err)
	}
	if err := r.CompleteFetch(feed.ID, "v2", "", true); err != nil {
		t.Fatal(err)
	}
	got, _ = r.GetByID(feed.ID)
	if got.FetchFailures != 0 || got.FetchError != "" || got.NextAttemptAt == nil || got.ETag != "" {
		t.Fatalf("pending state: %+v", got)
	}
	if err := r.CompleteFetch(feed.ID, "v2", "", false); err != nil {
		t.Fatal(err)
	}
	got, _ = r.GetByID(feed.ID)
	if got.NextAttemptAt != nil || got.ETag != "v2" {
		t.Fatalf("recovered: %+v", got)
	}
}
func TestSubscriptionDuplicateKeepsTransactionUsable(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var fid int
	if err := db.QueryRow(`INSERT INTO feeds(url,title) VALUES('https://example.org/rss','test') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	r := (&ArticleRepository{db: tx})
	for i := 0; i < 2; i++ {
		created, err := r.CreateSubscriptionArticle(&model.Article{FeedID: fid, Title: "test", URL: "https://example.org/article", Content: "body"})
		if err != nil || created != (i == 0) {
			t.Fatalf("insert %d created=%t err=%v", i, created, err)
		}
	}
	if _, err := tx.Exec(`UPDATE feeds SET title='still usable' WHERE id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func TestFetchedContentRejectsStaleWriter(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var fid int
	db.QueryRow(`INSERT INTO feeds(url,title) VALUES('https://example.org/rss','test') RETURNING id`).Scan(&fid)
	r := NewArticleRepository(db)
	a := model.Article{FeedID: fid, URL: "https://example.org/a", Title: "a", Content: strings.Repeat("old ", 100)}
	if err := r.Create(&a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE articles SET content='new capture',summary_brief='new summary' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	replaced, err := r.ReplaceFetchedContent(a.ID, a.Content, "slow old fetch", 1, 1)
	if err != nil || replaced {
		t.Fatalf("stale writer accepted=%t err=%v", replaced, err)
	}
	var body, summary string
	db.QueryRow(`SELECT content,summary_brief FROM articles WHERE id=$1`, a.ID).Scan(&body, &summary)
	if body != "new capture" || summary != "new summary" {
		t.Fatal("concurrent capture was overwritten")
	}
}

func TestContentUpdatesClearStaleSummariesAndRecoverQueuedCaptures(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var fid int
	if err := db.QueryRow(`INSERT INTO feeds(url,title) VALUES('https://example.org/rss','test') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	r := NewArticleRepository(db)
	for _, state := range []string{"queued", "failed"} {
		a := model.Article{FeedID: fid, URL: "https://example.org/" + state, Title: state, Content: "old", ProcessingState: state}
		if err := r.Create(&a); err != nil {
			t.Fatal(err)
		}
		db.Exec(`UPDATE articles SET summary_brief='old brief',summary_detailed='old detailed',processing_error='old error' WHERE id=$1`, a.ID)
		if state == "queued" {
			replaced, err := r.ReplaceFetchedContent(a.ID, "old", "new body", 2, 1)
			if err != nil || !replaced {
				t.Fatalf("replace=%t %v", replaced, err)
			}
		} else {
			if err := r.UpdateContent(a.ID, "new body", 2, 1); err != nil {
				t.Fatal(err)
			}
		}
		var brief, stateAfter, message string
		db.QueryRow(`SELECT COALESCE(summary_brief,''),processing_state,COALESCE(processing_error,'') FROM articles WHERE id=$1`, a.ID).Scan(&brief, &stateAfter, &message)
		if brief != "" || stateAfter != "ready" || message != "" {
			t.Fatalf("state=%s summary=%q error=%q", stateAfter, brief, message)
		}
	}
}
