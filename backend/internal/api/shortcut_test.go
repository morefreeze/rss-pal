package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestShortcutHTTPConnectionAndScope(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	h := api.NewShortcutHandler(repository.NewShortcutRepository(f.appDB))
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	g := f.router.Group("/api/settings/shortcuts", auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	g.GET("", h.Status)
	g.POST("/pair", h.Pair)
	g.DELETE("", h.Revoke)
	f.router.POST("/api/shortcuts/connect", h.Connect)
	feed := api.NewFeedHandler(repository.NewFeedRepository(f.appDB), repository.NewArticleRepository(f.appDB), "")
	f.router.POST("/api/shortcuts/capture", api.PublicTokenMiddleware(f.appDB, h.ResolveOwner), feed.CaptureFromShortcut)
	f.router.GET("/scope", api.PublicTokenMiddleware(f.appDB, h.ResolveOwner), func(c *gin.Context) {
		repo := repository.NewArticleRepository(f.appDB).WithCtx(c)
		a, err := repo.GetByID(f.articleA, f.userA)
		if err != nil || a == nil {
			c.Status(404)
			return
		}
		c.Status(200)
	})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	jwtA := signTestJWT(t, httpTestJWTSecret, f.userA, false)
	jwtB := signTestJWT(t, httpTestJWTSecret, f.userB, false)
	if w := request("POST", "/api/settings/shortcuts/pair", "", `{}`); w.Code != 401 {
		t.Fatalf("unauthenticated pair: %d", w.Code)
	}
	pair := request("POST", "/api/settings/shortcuts/pair", jwtA, `{}`)
	var p struct {
		Code string `json:"code"`
	}
	json.Unmarshal(pair.Body.Bytes(), &p)
	if pair.Code != 200 || len(p.Code) != 64 || pair.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("pair: %d %s", pair.Code, pair.Body.String())
	}
	exchange := request("POST", "/api/shortcuts/connect", "", `{"code":"`+p.Code+`"}`)
	var conn struct {
		Token string `json:"token"`
	}
	json.Unmarshal(exchange.Body.Bytes(), &conn)
	if exchange.Code != 200 || len(conn.Token) != 64 || exchange.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("connect: %d %s", exchange.Code, exchange.Body.String())
	}
	if w := request("POST", "/api/shortcuts/connect", "", `{"code":"`+p.Code+`"}`); w.Code != 401 {
		t.Fatalf("replay: %d", w.Code)
	}
	if w := request("GET", "/scope", conn.Token, ""); w.Code != 200 {
		t.Fatalf("capture scope could not see its owner's article: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/feeds/1", conn.Token, ""); w.Code != 401 {
		t.Fatalf("capture key accepted as login: %d", w.Code)
	}
	if w := request("POST", "/api/shortcuts/capture", jwtA, `{"url":"https://example.com"}`); w.Code != 401 {
		t.Fatalf("jwt accepted as shortcut key: %d", w.Code)
	}
	for _, u := range []string{"http://127.0.0.1/test", "file:///etc/passwd", "https://user:secret@example.com"} {
		body, _ := json.Marshal(map[string]string{"url": u})
		w := request("POST", "/api/shortcuts/capture", conn.Token, string(body))
		if w.Code != 400 {
			t.Fatalf("private URL: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request("DELETE", "/api/settings/shortcuts", jwtB, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/scope", conn.Token, ""); w.Code != 200 {
		t.Fatalf("other account revoked key: %d", w.Code)
	}
	if w := request("DELETE", "/api/settings/shortcuts", jwtA, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/shortcuts/capture", conn.Token, `{"url":"https://example.com"}`); w.Code != 401 {
		t.Fatalf("revoked: %d", w.Code)
	}
}
