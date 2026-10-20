package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

type shortcutContent struct{}

func (shortcutContent) FetchContent(context.Context, string) (string, error) {
	return "Shortcut captured body", nil
}
func (shortcutContent) FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error) {
	return rss.ContentResult{Content: "Shortcut captured body", Title: "Shortcut capture"}, nil
}
func (shortcutContent) FetchHTMLDocument(context.Context, string) (*goquery.Document, error) {
	return nil, nil
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
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		ID int `json:"parent_article_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	var owner int
	var content, url, feedType string
	var expanding bool
	err = db.QueryRow(`SELECT f.owner_id,a.content,a.url,f.feed_type,f.expand_links FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1`, result.ID).Scan(&owner, &content, &url, &feedType, &expanding)
	if err != nil || owner != uid || content != "Shortcut captured body" || expanding || feedType != "clip" || !strings.Contains(url, "x=1&y=2") {
		t.Fatalf("persisted owner=%d content=%q url=%q type=%q expanding=%v err=%v", owner, content, url, feedType, expanding, err)
	}
	var cost int
	if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=$1 AND bucket='capture'`, uid).Scan(&cost); err != nil || cost != 1 {
		t.Fatalf("capture budget cost=%d err=%v", cost, err)
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
