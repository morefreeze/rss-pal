package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	service "github.com/bytedance/rss-pal/internal/feedcatalog"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeedCatalogIsolationPublicationAndAuthority(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var admin, reader int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('catalog-admin','x',true) RETURNING id`).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('catalog-reader','x') RETURNING id`).Scan(&reader); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewFeedCatalogRepository(db)
	checks := 0
	fail := false
	svc := service.NewFeedCatalogService(repo, func(ctx context.Context, u string) error {
		checks++
		if fail {
			return errors.New("upstream unavailable")
		}
		return nil
	})
	h := NewFeedCatalogHandler(db, svc)
	uid := admin
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid) })
	r.GET("/feed-catalog", h.PublicList)
	g := r.Group("/admin/feed-catalog", h.RequireAdmin)
	g.GET("", h.List)
	g.POST("", h.Create)
	g.PUT("/:id", h.Update)
	g.POST("/:id/check", h.Check)
	g.POST("/:id/publication", h.Publication)
	request := func(method, path, body string, want int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	uid = reader
	request("POST", "/admin/feed-catalog", `{}`, 403)
	uid = 0
	request("GET", "/feed-catalog", "", 401)
	uid = admin
	body := `{"title":"Curated","url":"https://example.com/rss","category":"科技","description":"Independent","sort_order":1}`
	var item repository.FeedCatalogEntry
	if err := json.Unmarshal(request("POST", "/admin/feed-catalog", body, 201), &item); err != nil {
		t.Fatal(err)
	}
	request("POST", "/admin/feed-catalog", body, 409)
	if got := string(request("GET", "/feed-catalog", "", 200)); got != "[]" {
		t.Fatalf("draft exposed %s", got)
	}
	path := fmt.Sprintf("/admin/feed-catalog/%d", item.ID)
	fail = true
	request("POST", path+"/publication", fmt.Sprintf(`{"published":true,"revision":%d}`, item.Revision), 422)
	item, _ = repo.Get(context.Background(), item.ID)
	if item.Published || item.CheckStatus != "failed" {
		t.Fatalf("bad failure state %+v", item)
	}
	fail = false
	json.Unmarshal(request("POST", path+"/publication", fmt.Sprintf(`{"published":true,"revision":%d}`, item.Revision), 200), &item)
	if !item.Published || item.LastCheckedAt == nil {
		t.Fatal("not published after real check")
	}
	uid = reader
	var public []map[string]any
	json.Unmarshal(request("GET", "/feed-catalog", "", 200), &public)
	if len(public) != 1 || public[0]["last_error"] != nil {
		t.Fatalf("bad public shape %v", public)
	}
	request("POST", path+"/check", `{"revision":1}`, 403)
	uid = admin
	oldRev := item.Revision
	json.Unmarshal(request("PUT", path, fmt.Sprintf(`{"title":"Edited","url":"https://example.com/new","category":"科技","description":"","sort_order":2,"revision":%d}`, item.Revision), 200), &item)
	if item.Published || item.CheckStatus != "unchecked" || item.LastCheckedAt != nil {
		t.Fatal("URL edit retained publication/check")
	}
	request("POST", path+"/publication", fmt.Sprintf(`{"published":true,"revision":%d}`, oldRev), 409)
	if _, err := db.Exec(`UPDATE users SET is_admin=false WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	request("GET", "/admin/feed-catalog", "", 403)
	var count int
	db.QueryRow(`SELECT count(*) FROM feeds`).Scan(&count)
	if count != 0 {
		t.Fatalf("catalog created %d personal feeds", count)
	}
	if checks != 2 {
		t.Fatalf("unauthorized or stale request fetched: %d", checks)
	}
}
func TestFeedCatalogRejectsStaleCheckAndUnsafeURLs(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewFeedCatalogRepository(db)
	svc := service.NewFeedCatalogService(repo, func(ctx context.Context, u string) error { return nil })
	for _, u := range []string{"http://127.0.0.1/rss", "http://rsshub:1200/foo", "https://u:p@example.com/rss", "file:///etc/passwd"} {
		if _, err := svc.Save(context.Background(), 0, 0, repository.FeedCatalogInput{Title: "Bad", URL: u, Category: "Test"}); err == nil {
			t.Fatalf("accepted %s", u)
		}
	}
	item, err := svc.Save(context.Background(), 0, 0, repository.FeedCatalogInput{Title: "Good", URL: "https://example.com/rss", Category: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	svc = service.NewFeedCatalogService(repo, func(ctx context.Context, u string) error {
		_, err := svc.Save(ctx, item.ID, 0, repository.FeedCatalogInput{Title: "Changed", URL: "https://example.com/new", Category: "Test", Revision: item.Revision})
		return err
	})
	if _, err := svc.Check(context.Background(), item.ID, 0, item.Revision, true); !errors.Is(err, repository.ErrCatalogConflict) {
		t.Fatalf("stale check = %v", err)
	}
	got, _ := repo.Get(context.Background(), item.ID)
	if got.Published || got.CheckStatus != "unchecked" {
		t.Fatalf("stale result applied %+v", got)
	}
}
