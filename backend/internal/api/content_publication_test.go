package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

func TestContentRefetchPersistsPublicationDateWithoutOverwriting(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	var uid, fid, aid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('date-owner','unused') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('bookmarklet://user/1','clip',$1) RETURNING id`, uid).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,url,title,content,is_clip,media_url,media_type) VALUES($1,'https://example.com/a','article','old',true,'https://example.com/audio.mp3','audio/mpeg') RETURNING id`, fid).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2023, 6, 23, 0, 0, 0, 0, time.UTC)
	fetcher := &freshRecordingContentFetcher{publishedAt: &date}
	h := NewContentHandler(repository.NewArticleRepository(app), repository.NewFeedRepository(app), rss.NewFetcher(""))
	h.contentFetcher = fetcher
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid) }, RLSTxMiddleware(app))
	r.POST("/articles/:id/content", h.FetchContent)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/articles/%d/content", aid), nil))
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var got time.Time
		var content string
		if err := db.QueryRow(`SELECT published_at,content FROM articles WHERE id=$1`, aid).Scan(&got, &content); err != nil {
			t.Fatal(err)
		}
		if got.Format(time.RFC3339) != "2023-06-23T00:00:00Z" || content != "fresh Reader content" {
			t.Fatalf("persisted %v %q", got, content)
		}
		newDate := date.AddDate(1, 0, 0)
		fetcher.publishedAt = &newDate
	}
}
