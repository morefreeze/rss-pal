package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalSummaryResultsAreOwnedBoundedAndContentChecked(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	if _, err := f.privDB.Exec(`UPDATE articles SET content='original article body',summary_brief='old',summary_detailed='old detailed' WHERE id=$1`, f.articleA); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewArticleRepository(f.appDB)
	repo.SetSummaryJobDB(f.privDB)
	h := api.NewArticleHandler(repo, nil, nil, nil, nil, nil, nil)
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	group := f.router.Group("/api")
	group.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	group.GET("/articles/:id/summary/input", h.LocalSummaryInput)
	group.PUT("/articles/:id/summary/result", h.SaveLocalSummary)
	path := fmt.Sprintf("/api/articles/%d/summary", f.articleA)
	input := f.do(t, "GET", path+"/input", f.userA)
	if input.Code != 200 {
		t.Fatalf("input %d %s", input.Code, input.Body)
	}
	var prompts struct {
		ContentHash    string `json:"content_hash"`
		SummaryVersion string `json:"summary_version"`
		Brief          string `json:"brief_prompt"`
		Detailed       string `json:"detailed_prompt"`
	}
	if err := json.Unmarshal(input.Body.Bytes(), &prompts); err != nil {
		t.Fatal(err)
	}
	if prompts.ContentHash == "" || !strings.Contains(prompts.Brief, "original article body") || !strings.Contains(prompts.Detailed, "依据") {
		t.Fatalf("prompts %+v", prompts)
	}
	save := func(user int, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("PUT", path+"/result", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, user, false))
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	body := map[string]any{"content_hash": prompts.ContentHash, "summary_version": prompts.SummaryVersion, "summary_brief": "new brief", "summary_detailed": "new detailed"}
	if w := f.do(t, "GET", path+"/input", f.userB); w.Code != 404 {
		t.Fatalf("input leak %d %s", w.Code, w.Body)
	}
	if w := save(f.userB, body); w.Code != 404 {
		t.Fatalf("write leak %d %s", w.Code, w.Body)
	}
	body["api_key"] = "never-accept-secret"
	if w := save(f.userA, body); w.Code != 400 || strings.Contains(w.Body.String(), "never-accept-secret") {
		t.Fatalf("secret field %d %s", w.Code, w.Body)
	}
	delete(body, "api_key")
	body["summary_brief"] = " "
	if w := save(f.userA, body); w.Code != 400 {
		t.Fatalf("empty %d %s", w.Code, w.Body)
	}
	body["summary_brief"] = strings.Repeat("x", 20001)
	if w := save(f.userA, body); w.Code != 400 {
		t.Fatalf("oversize %d %s", w.Code, w.Body)
	}
	body["summary_brief"] = "new brief"
	body["content_hash"] = "stale"
	if w := save(f.userA, body); w.Code != 409 {
		t.Fatalf("stale %d %s", w.Code, w.Body)
	}
	body["content_hash"] = prompts.ContentHash
	if w := save(f.userA, body); w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	// A second tab still carrying the old summary revision must not overwrite it.
	if w := save(f.userA, body); w.Code != 409 {
		t.Fatalf("superseded %d %s", w.Code, w.Body)
	}
	var brief string
	if err := f.privDB.QueryRow(`SELECT summary_brief FROM articles WHERE id=$1`, f.articleA).Scan(&brief); err != nil || brief != "new brief" {
		t.Fatalf("saved %q %v", brief, err)
	}
}

func TestLocalSummaryCommitFailurePreservesPreviousResult(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	if _, err := f.privDB.Exec(`UPDATE articles SET content='body',summary_brief='old',summary_detailed='old detailed' WHERE id=$1`, f.articleA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`CREATE TABLE local_summary_commit_probe(uid integer REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED); GRANT INSERT ON local_summary_commit_probe TO rsspal_app`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewArticleRepository(f.appDB)
	repo.SetSummaryJobDB(f.privDB)
	h := api.NewArticleHandler(repo, nil, nil, nil, nil, nil, nil)
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	group := f.router.Group("/api")
	group.Use(auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	group.GET("/articles/:id/summary/input", h.LocalSummaryInput)
	group.PUT("/articles/:id/summary/result", func(c *gin.Context) {
		if _, err := c.MustGet(api.CtxKeyTx).(*sql.Tx).Exec(`INSERT INTO local_summary_commit_probe VALUES(-999)`); err != nil {
			t.Fatal(err)
		}
		h.SaveLocalSummary(c)
	})
	path := fmt.Sprintf("/api/articles/%d/summary", f.articleA)
	input := f.do(t, "GET", path+"/input", f.userA)
	var body map[string]any
	if err := json.Unmarshal(input.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	delete(body, "brief_prompt")
	delete(body, "detailed_prompt")
	body["summary_brief"] = "new brief"
	body["summary_detailed"] = "new detailed"
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", path+"/result", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, f.userA, false))
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 500 || strings.Contains(w.Body.String(), "new brief") {
		t.Fatalf("false success %d %s", w.Code, w.Body)
	}
	var brief, state string
	if err := f.privDB.QueryRow(`SELECT a.summary_brief,j.state FROM articles a JOIN article_summary_jobs j ON j.article_id=a.id WHERE a.id=$1`, f.articleA).Scan(&brief, &state); err != nil || brief != "old" || state != "failed" {
		t.Fatalf("brief=%q state=%q err=%v", brief, state, err)
	}
}
