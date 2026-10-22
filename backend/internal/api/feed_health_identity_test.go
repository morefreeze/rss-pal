package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestFeedHealthSourceIdentityAndTitleFallback(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	for _, title := range []any{"", "  ", nil} {
		url := "https://example.com/" + itoa(lenMustCountFeeds(t, f))
		if _, err := f.privDB.Exec(`INSERT INTO feeds (url,title,owner_id) VALUES ($1,$2,$3)`, url, title, f.userA); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.privDB.Exec(`UPDATE feeds SET status='archived', title='  ' WHERE id=$1`, f.privateFeedA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`INSERT INTO feeds (url,title,feed_type,owner_id) VALUES ('extension://twitter:user/1/alice','X 用户 · @alice','twitter:user',$1), ('https://private.example/feed','Other user secret','rss',$2)`, f.userA, f.userB); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), nil)
	r.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	r.GET("/api/feeds/health", api.NewFeedHealthHandler(repository.NewFeedHealthRepository(f.appDB), repository.NewFeedRepository(f.appDB)).Get)
	req := httptest.NewRequest(http.MethodGet, "/api/feeds/health", nil)
	req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, f.userA, false))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Rows []struct {
			Title string `json:"feed_title"`
			URL   string `json:"feed_url"`
			Type  string `json:"feed_type"`
		}
		Archived []struct {
			Title string `json:"feed_title"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 4 {
		t.Fatalf("rows=%+v", result.Rows)
	}
	for _, row := range result.Rows {
		if row.URL == "" || row.Type == "" {
			t.Errorf("missing source identity: %+v", row)
		}
		if row.Type == "twitter:user" {
			if row.Title != "X 用户 · @alice" {
				t.Errorf("title=%q", row.Title)
			}
		} else if row.Title != row.URL {
			t.Errorf("empty title must fall back to URL: %+v", row)
		}
	}
	if len(result.Archived) != 1 || result.Archived[0].Title != "http://a" {
		t.Errorf("archived=%+v", result.Archived)
	}
}

func lenMustCountFeeds(t *testing.T, f *httpLeakFixture) int {
	t.Helper()
	var count int
	if err := f.privDB.QueryRow(`SELECT count(*) FROM feeds`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
