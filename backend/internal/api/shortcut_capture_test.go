package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

// Any network fetch here would make a slow publisher block enqueue again.
type shortcutContent struct{}

func (shortcutContent) FetchContent(context.Context, string) (string, error) {
	panic("shortcut enqueue must not fetch content")
}

func TestConcurrentShortcutSharesCreateOneFirstClip(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanupApp := testdb.NewAsApp(t, schema)
	defer cleanupApp()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('concurrent-queue','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewShortcutRepository(db)
	code, _, err := repo.Pair(uid)
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	start := make(chan struct{})
	done := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(`{"url":"https://example.com/concurrent"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results <- w
		}()
	}
	close(start)
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent owner locks deadlocked")
	}
	close(results)
	var first int
	for w := range results {
		if w.Code != 202 {
			t.Fatalf("concurrent enqueue: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			ID int `json:"parent_article_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &result)
		if first != 0 && first != result.ID {
			t.Fatalf("duplicate articles %d vs %d", first, result.ID)
		}
		first = result.ID
	}
	var feeds, articles int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1 AND feed_type='clip'`, uid).Scan(&feeds); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE url='https://example.com/concurrent'`).Scan(&articles); err != nil {
		t.Fatal(err)
	}
	if feeds != 1 || articles != 1 {
		t.Fatalf("feeds=%d articles=%d", feeds, articles)
	}
}
func (shortcutContent) FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error) {
	panic("shortcut enqueue must not fetch content or metadata")
}
func (shortcutContent) FetchHTMLDocument(context.Context, string) (*goquery.Document, error) {
	panic("shortcut enqueue must not fetch HTML")
}

func shortcutCaptureRouter(db, budgetDB *sql.DB) *gin.Engine {
	h := NewShortcutHandler(repository.NewShortcutRepository(db))
	feed := NewFeedHandler(repository.NewFeedRepository(db), repository.NewArticleRepository(db), "")
	feed.contentFetcher = shortcutContent{}
	r := gin.New()
	r.POST("/api/shortcuts/connect", h.Connect)
	policies, _ := taskbudget.LoadPolicies()
	r.POST("/api/shortcuts/capture", PublicTokenMiddleware(db, h.ResolveOwner), TaskBudgetMiddleware(taskbudget.New(budgetDB), policies), feed.CaptureFromShortcut)
	return r
}
func TestShortcutCapturePersistsOnlyInOwnerClip(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('capture-owner','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewShortcutRepository(db)
	code, _, _ := repo.Pair(uid)
	token, err := repo.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(`{"url":"https://example.com/shared?x=1&y=2#片段","expand":true,"owner_id":999}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		ID int `json:"parent_article_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	var owner int
	var content, url, feedType, state string
	var expanding bool
	err = db.QueryRow(`SELECT f.owner_id,a.content,a.url,f.feed_type,f.expand_links,a.processing_state FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1`, result.ID).Scan(&owner, &content, &url, &feedType, &expanding, &state)
	if err != nil || owner != uid || content != "" || state != "queued" || expanding || feedType != "clip" || !strings.Contains(url, "x=1&y=2") {
		t.Fatalf("persisted owner=%d content=%q url=%q type=%q expanding=%v err=%v", owner, content, url, feedType, expanding, err)
	}
	var cost int
	if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=$1 AND bucket='capture'`, uid).Scan(&cost); err != nil || cost != 1 {
		t.Fatalf("capture budget cost=%d err=%v", cost, err)
	}
	// Pending, failed, and already saved shares must all reuse this row.
	for _, previous := range []string{"queued", "failed", "ready"} {
		if _, err := db.Exec(`UPDATE articles SET processing_state=$2, content='preserved body', refetch_attempts=3, processing_error='old error' WHERE id=$1`, result.ID, previous); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(`{"url":"https://example.com/shared?x=1&y=2#片段"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 202 && w.Code != 200 {
			t.Fatalf("repeat %s: %d %s", previous, w.Code, w.Body.String())
		}
		var next struct {
			ID int `json:"parent_article_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &next)
		var attempts, count int
		var processingError string
		if err := db.QueryRow(`SELECT processing_state, content, refetch_attempts, processing_error FROM articles WHERE id=$1`, next.ID).Scan(&state, &content, &attempts, &processingError); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=(SELECT feed_id FROM articles WHERE id=$1)`, next.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if next.ID != result.ID || count != 1 || content != "preserved body" || (previous == "ready" && state != "ready") || (previous == "failed" && (state != "queued" || attempts != 0 || processingError != "")) {
			t.Fatalf("repeat %s: id=%d count=%d state=%s content=%q attempts=%d error=%q", previous, next.ID, count, state, content, attempts, processingError)
		}
	}
}

func TestShortcutCaptureCommitFailureNeverReturnsSuccess(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('commit-failure','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewShortcutRepository(db)
	code, _, _ := repo.Pair(uid)
	token, err := repo.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION reject_shortcut_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced test commit failure'; END $$;
 CREATE CONSTRAINT TRIGGER reject_shortcut_commit AFTER INSERT ON articles DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_shortcut_commit();`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(`{"url":"https://example.com/commit-failure"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	shortcutCaptureRouter(app, db).ServeHTTP(w, req)
	if w.Code != 500 || strings.Contains(w.Body.String(), "parent_article_id") {
		t.Fatalf("failed commit returned success: %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE url='https://example.com/commit-failure'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed capture persisted: count=%d err=%v", n, err)
	}
}
