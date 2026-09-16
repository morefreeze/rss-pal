package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestFeedCreationDefaultsPrivateForEveryRole(t *testing.T) {
	for _, admin := range []bool{false, true} {
		name := "ordinary"
		if admin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			f, cleanup := newHTTPLeakFixture(t)
			defer cleanup()
			auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), nil)
			feeds := api.NewFeedHandler(repository.NewFeedRepository(f.appDB), repository.NewArticleRepository(f.appDB), "")
			r := gin.New()
			r.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
			r.POST("/feeds", feeds.Create)
			req := httptest.NewRequest(http.MethodPost, "/feeds", strings.NewReader(`{"url":"https://example.com/private-default.xml","feed_type":"rss"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, f.userA, admin))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", w.Code, w.Body.String())
			}
			var feed model.Feed
			if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
				t.Fatal(err)
			}
			if feed.OwnerID == nil || *feed.OwnerID != f.userA {
				t.Errorf("feed is not private to creator: owner=%v", feed.OwnerID)
			}
			other := f.do(t, http.MethodGet, "/api/feeds/"+itoa(feed.ID), f.userB)
			if other.Code != http.StatusNotFound {
				t.Errorf("another user can read new feed: %d", other.Code)
			}
			own := f.do(t, http.MethodGet, "/api/feeds/"+itoa(feed.ID), f.userA)
			if own.Code != http.StatusOK {
				t.Errorf("creator cannot read own feed: %d", own.Code)
			}
		})
	}
}
