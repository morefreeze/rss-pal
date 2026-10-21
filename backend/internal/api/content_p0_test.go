package api

import (
	"context"
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type p0ContentFetcher struct{ content string }

func (f *p0ContentFetcher) FetchContentFreshWithMetadata(context.Context, string) (rss.ContentResult, error) {
	return rss.ContentResult{Content: f.content}, nil
}
func (f *p0ContentFetcher) FindMediaInHTML(context.Context, string) *rss.MediaInfo { return nil }
func TestContentRefetchProtectsBodyAndInvalidatesChangedSummary(t *testing.T) {
	for _, tc := range []struct {
		name, old, next string
		wantCode        int
		wantSummary     bool
	}{
		{"truncated", strings.Repeat("完整正文。", 150), "Subscribe to continue reading", 422, true},
		{"replaced", "original body", "updated original body", 200, false},
		{"unchanged", "original body", "original body", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			var uid, fid, aid int
			if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('p0','x') RETURNING id`).Scan(&uid); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://example.org/feed','test',$1) RETURNING id`, uid).Scan(&fid); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,media_url,media_type) VALUES($1,'title','https://example.org/article',$2,'brief','detailed','https://example.org/a.mp3','audio/mpeg') RETURNING id`, fid, tc.old).Scan(&aid); err != nil {
				t.Fatal(err)
			}
			h := NewContentHandler(repository.NewArticleRepository(db), repository.NewFeedRepository(db), rss.NewFetcher(""))
			h.contentFetcher = &p0ContentFetcher{tc.next}
			h.fetcher = nil
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set("userID", uid) }, RLSTxMiddleware(db))
			r.POST("/articles/:id/content", h.FetchContent)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/articles/%d/content", aid), nil))
			if w.Code != tc.wantCode {
				t.Errorf("status=%d, want %d: %s", w.Code, tc.wantCode, w.Body.String())
			}
			var body, summary string
			if err := db.QueryRow(`SELECT content,COALESCE(summary_brief,'') FROM articles WHERE id=$1`, aid).Scan(&body, &summary); err != nil {
				t.Fatal(err)
			}
			want := tc.next
			if tc.wantCode != 200 {
				want = tc.old
			}
			if body != want {
				t.Error("stored body was not protected/replaced correctly")
			}
			if (summary != "") != tc.wantSummary {
				t.Errorf("summary retained=%t want %t", summary != "", tc.wantSummary)
			}
		})
	}
}
func TestManualNotModifiedAdvancesLastCheck(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var uid, fid int
	db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('manual304','x') RETURNING id`).Scan(&uid)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) }))
	defer server.Close()
	before := time.Now().UTC().Add(-2 * time.Hour)
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id,last_fetched_at,etag) VALUES($1,'manual',$2,$3,'old') RETURNING id`, server.URL, uid, before).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	h := NewFeedHandler(repository.NewFeedRepository(db), repository.NewArticleRepository(db), "")
	h.fetcher = rss.NewFetcher("")
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid) }, RLSTxMiddleware(db))
	r.POST("/feeds/:id/fetch", h.FetchNow)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/feeds/%d/fetch", fid), nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var checked time.Time
	var etag string
	db.QueryRow(`SELECT last_fetched_at,etag FROM feeds WHERE id=$1`, fid).Scan(&checked, &etag)
	if !checked.After(before) || etag != "old" {
		t.Fatalf("304 check=%v etag=%q", checked, etag)
	}
}

func TestManualSubscriptionDrainsConditionalBatch(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var uid, fid int
	db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('manual-batch','x') RETURNING id`).Scan(&uid)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"batch"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"batch"`)
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Batch</title><link>https://example.org</link><description>Batch</description>`)
		for i := 0; i < 13; i++ {
			fmt.Fprintf(w, `<item><title>Article</title><link>https://example.org/%d</link><description>Body</description></item>`, i)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer server.Close()
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id,feed_type) VALUES($1,'manual',$2,'podcast') RETURNING id`, server.URL, uid).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	h := NewFeedHandler(repository.NewFeedRepository(db), repository.NewArticleRepository(db), "")
	h.fetcher = rss.NewFetcher("")
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid) }, RLSTxMiddleware(db))
	r.POST("/feeds/:id/fetch", h.FetchNow)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/feeds/%d/fetch", fid), nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=$1`, fid).Scan(&count)
	if count != 13 {
		t.Fatalf("manual batch=%d want13", count)
	}
}
