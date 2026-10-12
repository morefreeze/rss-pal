package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

func TestContentFetchCommitsDespiteSlowMediaBackfill(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer source.Close()
	var uid, fid, aid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('content-owner','unused') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES($1,'source',$2) RETURNING id`, source.URL, uid).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,url,title,content) VALUES($1,'https://example.com/article','article','summary...') RETURNING id`, fid).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	h := NewContentHandler(repository.NewArticleRepository(db), repository.NewFeedRepository(db), rss.NewFetcher(""))
	h.contentFetcher = &freshRecordingContentFetcher{}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid) }, RLSTxMiddleware(db))
	r.POST("/articles/:id/content", h.FetchContent)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req := httptest.NewRequest("POST", fmt.Sprintf("/articles/%d/content", aid), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if ctx.Err() != nil {
		t.Errorf("optional media lookup exhausted request deadline: %v", ctx.Err())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got string
	if err := db.QueryRow(`SELECT content FROM articles WHERE id=$1`, aid).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "fresh Reader content" {
		t.Fatalf("content not committed: %q", got)
	}
}
