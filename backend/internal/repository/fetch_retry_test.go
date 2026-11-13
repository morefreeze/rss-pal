package repository_test

import (
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestBackgroundFetchRetryWindow(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	repo := repository.NewArticleRepository(db)
	var id int
	if err := db.QueryRow(`WITH feed AS (INSERT INTO feeds(url,title) VALUES ('https://example.test/feed','Retry') RETURNING id)
		INSERT INTO articles(feed_id,url,title,content) SELECT id,'https://example.test/item','Item','short' FROM feed RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeferContentRetry(id); err != nil {
		t.Fatal(err)
	}
	var delay float64
	if err := db.QueryRow(`SELECT extract(epoch FROM refetch_next_attempt_at-refetch_retry_started_at) FROM articles WHERE id=$1`, id).Scan(&delay); err != nil || delay != 120 {
		t.Fatalf("first delay=%v err=%v", delay, err)
	}
	if rows, err := repo.GetArticlesWithShortContent(300); err != nil || len(rows) != 0 {
		t.Fatalf("deferred content was eligible: %d, %v", len(rows), err)
	}
	if _, err := db.Exec(`UPDATE articles SET refetch_next_attempt_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeferContentRetry(id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT extract(epoch FROM refetch_next_attempt_at-now()) FROM articles WHERE id=$1`, id).Scan(&delay); err != nil || delay < 239 || delay > 240 {
		t.Fatalf("second delay=%v err=%v", delay, err)
	}
	if _, err := db.Exec(`UPDATE articles SET refetch_retry_started_at=now()-interval '72 hours', refetch_next_attempt_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.GetArticlesWithShortContent(300); err != nil || len(rows) != 0 {
		t.Fatalf("expired content was eligible: %d, %v", len(rows), err)
	}

	var child int
	if err := db.QueryRow(`INSERT INTO articles(feed_id,url,title,parent_article_id,processing_state,refetch_retry_started_at)
		SELECT feed_id,'https://example.test/child','Child',id,'processing',now()-interval '73 hours' FROM articles WHERE id=$1 RETURNING id`, id).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.GetProcessingChildren(10); err != nil || len(rows) != 0 {
		t.Fatalf("expired child was eligible: %d, %v", len(rows), err)
	}
	var state string
	if err := db.QueryRow(`SELECT processing_state FROM articles WHERE id=$1`, child).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("expired child state=%s err=%v", state, err)
	}
	if count, err := repo.QueueLinkSetChild(child); err != nil || count != 1 {
		t.Fatalf("manual retry count=%d err=%v", count, err)
	}
	if rows, err := repo.GetProcessingChildren(10); err != nil || len(rows) != 1 {
		t.Fatalf("manual retry was not queued: %d, %v", len(rows), err)
	}

	if _, err := db.Exec(`UPDATE articles SET media_type='video/youtube' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeferTranscriptRetry(id, false); err != nil {
		t.Fatal(err)
	}
	var next time.Time
	if err := db.QueryRow(`SELECT transcript_next_attempt_at FROM articles WHERE id=$1`, id).Scan(&next); err != nil || time.Until(next) < time.Minute {
		t.Fatalf("transcript next=%v err=%v", next, err)
	}
	if _, err := db.Exec(`UPDATE articles SET transcript_retry_started_at=now()-interval '73 hours', transcript_next_attempt_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.GetMediaArticlesWithoutTranscript(10); err != nil || len(rows) != 0 {
		t.Fatalf("expired transcript was eligible: %d, %v", len(rows), err)
	}
}
