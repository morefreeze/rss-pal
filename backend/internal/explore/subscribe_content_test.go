package explore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
)

func TestSubscribeCopiesFetchedOriginalAndPreservesBetterExistingBody(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Now().UTC()
	uid := seedSubscribeUser(t, db, "full-content-copy")
	sid := seedSubscribeSource(t, db, uid, "https://full-copy.example/feed", "Full", "valid", now)
	var aid int
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,content_version,fetched_at) VALUES($1,'https://full-copy.example/post','https://full-copy.example/post','Post','RSS summary',1,$2) RETURNING id`, sid, now).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("完整正文 ", 500)
	if _, err := db.Exec(`INSERT INTO explore_article_content(article_id,source_url,content,status,lease_token) VALUES($1,'https://full-copy.example/post',$2,'fetched','test')`, aid, full); err != nil {
		t.Fatal(err)
	}
	service := NewSubscribeService(db).WithContentCache(db)
	first, err := service.SubscribeOne(uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err = db.QueryRow(`SELECT content FROM articles WHERE feed_id=$1`, first.FeedID).Scan(&body); err != nil || body != full {
		t.Fatalf("copied chars=%d want=%d err=%v", len(body), len(full), err)
	}
	better := full + "reader recovered the conclusion"
	if _, err = db.Exec(`UPDATE articles SET content=$2,summary_brief='keep brief',summary_detailed='keep detailed',word_count=999,reading_minutes=99 WHERE feed_id=$1`, first.FeedID, better); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SubscribeOne(uid, sid); err != nil {
		t.Fatal(err)
	}
	var brief, detailed sql.NullString
	var words, minutes int
	if err = db.QueryRow(`SELECT content,summary_brief,summary_detailed,word_count,reading_minutes FROM articles WHERE feed_id=$1`, first.FeedID).Scan(&body, &brief, &detailed, &words, &minutes); err != nil {
		t.Fatal(err)
	}
	if body != better || brief.String != "keep brief" || detailed.String != "keep detailed" || words != 999 || minutes != 99 {
		t.Fatalf("better article overwritten body=%d brief=%v detailed=%v metrics=%d/%d", len(body), brief, detailed, words, minutes)
	}
	upgraded := better + "\nnew verified section"
	if _, err = db.Exec(`UPDATE explore_article_content SET content=$2 WHERE article_id=$1`, aid, upgraded); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SubscribeOne(uid, sid); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT content,summary_brief,summary_detailed,word_count,reading_minutes FROM articles WHERE feed_id=$1`, first.FeedID).Scan(&body, &brief, &detailed, &words, &minutes); err != nil {
		t.Fatal(err)
	}
	wantWords, wantMinutes := rss.ComputeMetrics(upgraded)
	if body != upgraded || brief.Valid || detailed.Valid || words != wantWords || minutes != wantMinutes {
		t.Fatal("improvement did not replace body and invalidate summaries")
	}
}

func TestSubscribeIgnoresMismatchedFailedAndShorterCachedContent(t *testing.T) {
	for _, tc := range []struct{ name, url, state, body string }{{"changed URL", "https://old.example/post", "fetched", strings.Repeat("old", 100)}, {"failed", "https://cache-test.example/post", "failed", strings.Repeat("bad", 100)}, {"shorter", "https://cache-test.example/post", "fetched", "tiny"}} {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			now := time.Now().UTC()
			uid := seedSubscribeUser(t, db, "cache-"+tc.name)
			sid := seedSubscribeSource(t, db, uid, "https://cache-test.example/feed", "Cache", "valid", now)
			var aid int
			if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES($1,'https://cache-test.example/post','https://cache-test.example/post','Post','complete RSS body',$2) RETURNING id`, sid, now).Scan(&aid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO explore_article_content(article_id,source_url,content,status,lease_token) VALUES($1,$2,$3,$4,'t')`, aid, tc.url, tc.body, tc.state); err != nil {
				t.Fatal(err)
			}
			got, err := NewSubscribeService(db).WithContentCache(db).SubscribeOne(uid, sid)
			if err != nil {
				t.Fatal(err)
			}
			var body string
			if err = db.QueryRow(`SELECT content FROM articles WHERE feed_id=$1`, got.FeedID).Scan(&body); err != nil || body != "complete RSS body" {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}

func TestSubscribeOriginalCacheKeepsRequestRLSAndAuthorization(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, done := testdb.NewAsApp(t, schema)
	defer done()
	now := time.Now().UTC()
	uid := seedSubscribeUser(t, db, "rls-copy-original")
	other := seedSubscribeUser(t, db, "rls-copy-other")
	sid := seedSubscribeSource(t, db, uid, "https://rls-copy.example/feed", "RLS", "valid", now)
	var aid int
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES($1,'https://rls-copy.example/post','https://rls-copy.example/post','Post','intro',$2) RETURNING id`, sid, now).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("original body ", 50)
	if _, err := db.Exec(`INSERT INTO explore_article_content(article_id,source_url,content,status,lease_token) VALUES($1,'https://rls-copy.example/post',$2,'fetched','t')`, aid, full); err != nil {
		t.Fatal(err)
	}
	tx, err := app.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('app.user_id',$1,true)`, fmt.Sprint(uid)); err != nil {
		t.Fatal(err)
	}
	var cacheRows int
	if err = tx.QueryRow(`SELECT count(*) FROM explore_article_content`).Scan(&cacheRows); err != nil || cacheRows != 0 {
		t.Fatalf("app directly sees cache: %d %v", cacheRows, err)
	}
	service := NewSubscribeService(app).WithContentCache(db).WithQuerier(tx)
	result, err := service.SubscribeOne(uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err = tx.QueryRow(`SELECT content FROM articles WHERE feed_id=$1`, result.FeedID).Scan(&body); err != nil || body != full {
		t.Fatalf("authorized copy body=%q err=%v", body, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// The same admin cache cannot authorize a source hidden from another owner.
	tx2, err := app.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	if _, err = tx2.Exec(`SELECT set_config('app.user_id',$1,true)`, fmt.Sprint(other)); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSubscribeService(app).WithContentCache(db).WithQuerier(tx2).SubscribeOne(other, sid); !errors.Is(err, ErrSubscribeSourceUnavailable) {
		t.Fatalf("unauthorized source accepted: %v", err)
	}
}

func TestSubscribePreservesBodyCommittedByConcurrentWorker(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Now().UTC()
	uid := seedSubscribeUser(t, db, "concurrent-original")
	sid := seedSubscribeSource(t, db, uid, "https://concurrent-copy.example/feed", "Concurrent", "valid", now)
	if _, err := db.Exec(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES($1,'https://concurrent-copy.example/post','https://concurrent-copy.example/post','Post','RSS intro',$2)`, sid, now); err != nil {
		t.Fatal(err)
	}
	service := NewSubscribeService(db)
	first, err := service.SubscribeOne(uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Rollback()
	full := strings.Repeat("worker full text ", 100)
	if _, err = worker.Exec(`UPDATE articles SET content=$2,summary_brief='worker brief',word_count=300,reading_minutes=2 WHERE feed_id=$1`, first.FeedID, full); err != nil {
		t.Fatal(err)
	}
	var workerPID int
	if err = worker.QueryRow(`SELECT pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := service.SubscribeOne(uid, sid); done <- e }()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))`, workerPID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("subscription did not reach the worker-held row lock")
	}
	if err = worker.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscription did not finish")
	}
	var body, brief string
	var words, minutes int
	if err = db.QueryRow(`SELECT content,summary_brief,word_count,reading_minutes FROM articles WHERE feed_id=$1`, first.FeedID).Scan(&body, &brief, &words, &minutes); err != nil {
		t.Fatal(err)
	}
	if body != full || brief != "worker brief" || words != 300 || minutes != 2 {
		t.Fatal("subscription overwrote newly committed worker content")
	}
}
