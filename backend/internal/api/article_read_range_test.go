package api_test

import (
	"encoding/json"
	"fmt"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarkReadRangeHTTPWithRLS(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	h := api.NewArticleHandler(nil, nil, repository.NewProgressRepository(f.appDB), nil, nil, nil, nil)
	// Reuse the fixture's real auth and RLS middleware for this additional route.
	group := f.router.Group("/api/range-test")
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	group.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	group.POST("", h.MarkReadRange)
	send := func(user int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/range-test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != 0 {
			req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, user, false))
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	valid := fmt.Sprintf(`{"anchor_id":%d,"direction":"below"}`, f.articleA)
	if w := send(0, valid); w.Code != 401 {
		t.Fatalf("anonymous: %d %s", w.Code, w.Body)
	}
	if w := send(f.userB, valid); w.Code != 404 {
		t.Fatalf("foreign: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{}`, `{"anchor_id":1,"direction":"all"}`, `{"anchor_id":1,"direction":"below","sort":"bad"}`, `{"anchor_id":1,"direction":"below","feed_id":-1}`, `{"anchor_id":1,"direction":"below","source":"host:x"}`} {
		if w := send(f.userA, body); w.Code != 400 {
			t.Fatalf("bad request %s: %d", body, w.Code)
		}
	}
	for _, count := range []int{1, 0} {
		w := send(f.userA, valid)
		if w.Code != 200 {
			t.Fatalf("owned: %d %s", w.Code, w.Body)
		}
		var result struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Count != count {
			t.Fatalf("count=%d want=%d", result.Count, count)
		}
	}
	var completed bool
	if err := f.privDB.QueryRow(`SELECT is_completed FROM reading_progress WHERE article_id=$1 AND user_id=$2`, f.articleA, f.userA).Scan(&completed); err != nil || !completed {
		t.Fatalf("commit: completed=%v err=%v", completed, err)
	}
}
