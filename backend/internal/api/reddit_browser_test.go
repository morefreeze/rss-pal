package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeRedditStore struct {
	calls int
	err   error
}

func (s *fakeRedditStore) Ingest(context.Context, explore.RedditBrowserBatch, time.Time) (repository.RedditBrowserResult, error) {
	s.calls++
	return repository.RedditBrowserResult{Accepted: 1}, s.err
}
func TestRedditBrowserAuthenticationAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, token    string
		admin, invalid bool
		storeErr       error
		status, calls  int
	}{
		{name: "missing token", admin: true, status: 401},
		{name: "invalid token", token: "wrong", admin: true, status: 401},
		{name: "nonadmin", token: testBookmarkletToken, status: 403},
		{name: "bad body", token: testBookmarkletToken, admin: true, invalid: true, status: 400},
		{name: "accepted", token: testBookmarkletToken, admin: true, status: 200, calls: 1},
		{name: "disabled", token: testBookmarkletToken, admin: true, storeErr: repository.ErrRedditBrowserDisabled, status: 409, calls: 1},
		{name: "db failure", token: testBookmarkletToken, admin: true, storeErr: errors.New("private internal detail"), status: 500, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &ExtensionIngestHandler{userRepo: &stubExtUserRepo{token: testBookmarkletToken, user: &model.User{ID: 1, IsAdmin: tc.admin}}}
			store := &fakeRedditStore{err: tc.storeErr}
			h := NewRedditBrowserHandler(auth, store)
			r := gin.New()
			r.POST("/test", h.Ingest)
			b := explore.RedditBrowserBatch{Subreddit: "programming", Period: "week", CapturedAt: time.Now(), Listing: json.RawMessage(`{"kind":"Listing","data":{"children":[]}}`)}
			if tc.invalid {
				b.Period = "all"
			}
			body, _ := json.Marshal(b)
			request := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, request)
			if rec.Code != tc.status || store.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, store.calls, rec.Body)
			}
			if bytes.Contains(rec.Body.Bytes(), []byte("private internal")) {
				t.Fatal("leaked error detail")
			}
		})
	}
}

func (s *fakeRedditStore) RegisterSubreddit(_ context.Context, name string) (string, error) {
	s.calls++
	return name, s.err
}
func TestRegisterRedditSubreddit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		admin bool
		token string
		sub   string
		code  int
	}{
		{"no token", true, "", "golang", 401}, {"nonadmin", false, testBookmarkletToken, "golang", 403},
		{"invalid", true, testBookmarkletToken, "all", 400}, {"register", true, testBookmarkletToken, "GoLang", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &ExtensionIngestHandler{userRepo: &stubExtUserRepo{token: testBookmarkletToken, user: &model.User{ID: 1, IsAdmin: tc.admin}}}
			store := &fakeRedditStore{}
			h := NewRedditBrowserHandler(auth, store)
			r := gin.New()
			r.POST("/register", h.RegisterSubreddit)
			b, _ := json.Marshal(map[string]string{"subreddit": tc.sub})
			req := httptest.NewRequest("POST", "/register", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
			}
			if tc.code != 200 && store.calls != 0 {
				t.Fatal("unauthorized or invalid registration reached store")
			}
			if tc.code == 200 && !bytes.Contains(rec.Body.Bytes(), []byte(`"golang"`)) {
				t.Fatalf("not normalized: %s", rec.Body)
			}
		})
	}
}
