package api_test

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeedMutationsRespectOwnership(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	var publicID int
	if err := f.privDB.QueryRow(`INSERT INTO feeds(url,title) VALUES('https://public.test/feed','Public') RETURNING id`).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), nil)
	h := api.NewFeedHandler(repository.NewFeedRepository(f.appDB), repository.NewArticleRepository(f.appDB), "")
	r := gin.New()
	r.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	r.PATCH("/feeds/:id/status", h.UpdateStatus)
	r.PATCH("/feeds/:id/weight", h.UpdateWeight)
	for _, operation := range []struct{ path, body string }{{"status", `{"status":"paused"}`}, {"weight", `{"priority_weight":2}`}} {
		for _, tc := range []struct {
			name     string
			user, id int
			admin    bool
			want     int
		}{{"owner", f.userA, f.privateFeedA, false, 204}, {"other", f.userB, f.privateFeedA, false, 404}, {"public", f.userB, publicID, false, 403}, {"admin_public", f.userB, publicID, true, 204}} {
			t.Run(operation.path+"/"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest("PATCH", "/feeds/"+itoa(tc.id)+"/"+operation.path, strings.NewReader(operation.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, tc.user, tc.admin))
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != tc.want {
					t.Errorf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
				}
			})
		}
	}
}

func TestPrivateArticleImagesAuthenticatedRLS(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "article_images", itoa(f.articleA))
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "0.png"), []byte("private-image"), 0600); err != nil {
		t.Fatal(err)
	}
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), nil)
	h := api.NewArticleImageHandler(dir, api.ArticleImageAccess(repository.NewArticleRepository(f.appDB)))
	r := gin.New()
	r.Use(api.ArticleImageCacheControl(), auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	r.GET("/api/articles/:id/images/:idx", h.Serve)
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, api.Claims{UserID: f.userA, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}})
	expiredToken, err := expired.SignedString([]byte(httpTestJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"anonymous", "", 401}, {"owner", signTestJWT(t, httpTestJWTSecret, f.userA, false), 200},
		{"other", signTestJWT(t, httpTestJWTSecret, f.userB, false), 403},
		{"forged", signTestJWT(t, "wrong-secret", f.userA, false), 401}, {"expired", expiredToken, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/articles/"+itoa(f.articleA)+"/images/0.png", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Errorf("missing private cache policy on status %d", w.Code)
			}
			if tc.want == 200 && (w.Body.String() != "private-image" || w.Header().Get("Cache-Control") != "private, no-store") {
				t.Fatalf("wrong authorized body/cache: %s %v", w.Body.String(), w.Header())
			}
			if tc.want != 200 && strings.Contains(w.Body.String(), "private-image") {
				t.Fatal("private bytes leaked")
			}
		})
	}
}
