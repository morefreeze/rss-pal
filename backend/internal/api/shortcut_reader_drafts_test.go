package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func seedShortcutSource(t *testing.T, db *sql.DB, owner int, suffix, body string) int {
	t.Helper()
	var feed, id int
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES($1,'source',$2) RETURNING id`, "https://source.example/"+suffix, owner).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content) VALUES($1,'Original article',$2,$3) RETURNING id`, feed, "https://source.example/"+suffix+"/article", body).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestShortcutDraftListRequiresExplicitBatchConfirmation(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var a, b int
	for name, id := range map[string]*int{"owner-a": &a, "owner-b": &b} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	parent := seedShortcutSource(t, db, a, "a", "[First](https://example.com/first/#one) and [Second](https://example.com/second?utm_source=x) and [Homepage](https://homepage.example)")
	seedShortcutSource(t, db, b, "b", "[First](https://example.com/first/#two)")
	connections := repository.NewShortcutRepository(db)
	code, _, _ := connections.Pair(a)
	token, err := connections.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	reader := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
	g := router.Group("/api/articles", func(c *gin.Context) { n, _ := strconv.Atoi(c.GetHeader("X-Test-User")); c.Set("userID", n) }, RLSTxMiddleware(app))
	g.GET("/:id/capture-drafts", reader.CaptureDrafts)
	g.DELETE("/:id/capture-drafts", reader.DeleteCaptureDrafts)
	g.POST("/:id/batch_fetch", reader.BatchFetch)
	request := func(method, path, body string, user int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Test-User", strconv.Itoa(user))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, u := range []string{"https://example.com/first/#one", "https://example.com/second?utm_source=x"} {
		w := request("POST", "/api/shortcuts/capture", fmt.Sprintf(`{"url":%q}`, u), a)
		if w.Code != 202 {
			t.Fatalf("share %d %s", w.Code, w.Body.String())
		}
	}
	path := fmt.Sprintf("/api/articles/%d/capture-drafts", parent)
	w := request("GET", path, "", a)
	var list struct {
		Drafts  []repository.ArticleCaptureDraft `json:"drafts"`
		Fetched []string                         `json:"fetched_link_urls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(list.Drafts) != 2 || list.Drafts[0].URL != "https://example.com/first" || list.Drafts[0].Title != "First" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reader list %d %s", w.Code, w.Body.String())
	}
	if other := request("GET", path, "", b); other.Code != 404 {
		t.Fatalf("other owner can read drafts: %d %s", other.Code, other.Body.String())
	}
	if other := request("DELETE", path, `{"urls":["https://example.com/first"]}`, b); other.Code != 404 {
		t.Fatalf("other owner can remove drafts: %d", other.Code)
	}
	batch := request("POST", fmt.Sprintf("/api/articles/%d/batch_fetch", parent), `{"candidates":[{"url":"https://example.com/first","title":"First"}]}`, a)
	if batch.Code != 200 {
		t.Fatalf("confirm %d %s", batch.Code, batch.Body.String())
	}
	w = request("GET", path, "", a)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Drafts) != 1 || list.Drafts[0].URL != "https://example.com/second" || len(list.Fetched) != 1 {
		t.Fatalf("only confirmed draft should leave queue: %s", w.Body.String())
	}
	var state string
	if err := db.QueryRow(`SELECT processing_state FROM articles WHERE parent_article_id=$1`, parent).Scan(&state); err != nil || state != "processing" {
		t.Fatalf("confirmed child state=%s err=%v", state, err)
	}
	w = request("POST", "/api/shortcuts/capture", `{"url":"https://example.com/first/#two"}`, a)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "already_fetched") {
		t.Fatalf("fetched link requeued %d %s", w.Code, w.Body.String())
	}
	w = request("DELETE", path, `{"urls":["https://example.com/second"]}`, a)
	if w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body.String())
	}
	w = request("GET", path, "", a)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Drafts) != 0 {
		t.Fatalf("removed link returned %s", w.Body.String())
	}
}

func TestShortcutShareWaitsInOriginalArticleDraftList(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('draft-owner','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	parent := seedShortcutSource(t, db, owner, "draft", "Read [How I built this](https://example.com/shared#section) before deciding.")
	repo := repository.NewShortcutRepository(db)
	code, _, _ := repo.Pair(owner)
	token, err := repo.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(`{"url":"https://example.com/shared#section","expand":true}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var result struct {
			ID      int    `json:"parent_article_id"`
			Message string `json:"message"`
		}
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 202 || result.ID != parent || !strings.Contains(result.Message, "待抓取") {
			t.Fatalf("share should append to original article, status=%d source=%d want=%d body=%s", w.Code, result.ID, parent, w.Body.String())
		}
	}
	var drafts, clips, children int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE user_id=$1 AND article_id=$2`, owner, parent).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE f.feed_type='clip' OR a.parent_article_id=$1`, parent).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1 AND feed_type='clip'`, owner).Scan(&clips); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 || clips != 0 || children != 0 {
		t.Fatalf("drafts=%d clips=%d children=%d", drafts, clips, children)
	}
}

func TestShortcutHomepageDraftRoundtrip(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('homepage-owner','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	parent := seedShortcutSource(t, db, owner, "homepage", "[Homepage](https://homepage.example)")
	connections := repository.NewShortcutRepository(db)
	code, _, _ := connections.Pair(owner)
	token, err := connections.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	h := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
	g := router.Group("/reader", func(c *gin.Context) { c.Set("userID", owner) }, RLSTxMiddleware(app))
	g.GET("/:id/capture-drafts", h.CaptureDrafts)
	g.DELETE("/:id/capture-drafts", h.DeleteCaptureDrafts)
	g.POST("/:id/batch_fetch", h.BatchFetch)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := fmt.Sprintf("/reader/%d/capture-drafts", parent)
	for _, url := range []string{"https://homepage.example", "https://homepage.example/"} {
		w := request("POST", "/api/shortcuts/capture", fmt.Sprintf(`{"url":%q}`, url))
		if w.Code != 202 {
			t.Fatalf("share %d %s", w.Code, w.Body.String())
		}
	}
	w := request("GET", path, "")
	if !strings.Contains(w.Body.String(), `"url":"https://homepage.example/"`) {
		t.Fatalf("root identity differs from reader %s", w.Body.String())
	}
	if w := request("DELETE", path, `{"urls":["https://homepage.example/"]}`); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body.String())
	}
	w = request("GET", path, "")
	if !strings.Contains(w.Body.String(), `"drafts":[]`) {
		t.Fatalf("deleted root resurrected %s", w.Body.String())
	}
	request("POST", "/api/shortcuts/capture", `{"url":"https://homepage.example"}`)
	w = request("POST", fmt.Sprintf("/reader/%d/batch_fetch", parent), `{"candidates":[{"url":"https://homepage.example/","title":"Homepage"}]}`)
	if w.Code != 200 {
		t.Fatalf("confirm %d %s", w.Code, w.Body.String())
	}
	w = request("POST", "/api/shortcuts/capture", `{"url":"https://homepage.example"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "already_fetched") {
		t.Fatalf("root requeued %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1`, parent).Scan(&n); err != nil || n != 0 {
		t.Fatalf("root persisted after confirmation n=%d err=%v", n, err)
	}
}

func TestShortcutDraftMutationCommitFailurePreservesPending(t *testing.T) {
	for _, operation := range []string{"share", "remove", "confirm"} {
		t.Run(operation, func(t *testing.T) {
			db, schema, cleanup := testdb.NewWithSchema(t)
			defer cleanup()
			app, cleanapp := testdb.NewAsApp(t, schema)
			defer cleanapp()
			var owner int
			if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('rollback-owner','x') RETURNING id`).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			parent := seedShortcutSource(t, db, owner, "rollback", "[Pending](https://example.com/pending)")
			if operation != "share" {
				if _, err := db.Exec(`INSERT INTO article_capture_drafts(user_id,article_id,url,title) VALUES($1,$2,'https://example.com/pending','Pending')`, owner, parent); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`CREATE FUNCTION reject_draft_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test commit rejected'; END $$;
CREATE CONSTRAINT TRIGGER reject_draft_commit AFTER INSERT OR DELETE ON article_capture_drafts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_draft_commit();`); err != nil {
				t.Fatal(err)
			}
			connections := repository.NewShortcutRepository(db)
			code, _, err := connections.Pair(owner)
			if err != nil {
				t.Fatal(err)
			}
			token, err := connections.Connect(code)
			if err != nil {
				t.Fatal(err)
			}
			router := shortcutCaptureRouter(app, db)
			h := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
			g := router.Group("/reader", func(c *gin.Context) { c.Set("userID", owner) }, RLSTxMiddleware(app))
			g.DELETE("/:id/capture-drafts", h.DeleteCaptureDrafts)
			g.POST("/:id/batch_fetch", h.BatchFetch)
			method, path, body := "POST", "/api/shortcuts/capture", `{"url":"https://example.com/pending"}`
			if operation == "remove" {
				method, path, body = "DELETE", fmt.Sprintf("/reader/%d/capture-drafts", parent), `{"urls":["https://example.com/pending"]}`
			}
			if operation == "confirm" {
				path, body = fmt.Sprintf("/reader/%d/batch_fetch", parent), `{"candidates":[{"url":"https://example.com/pending","title":"Pending"}]}`
			}
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code < 500 || strings.Contains(w.Body.String(), `"inserted"`) || strings.Contains(w.Body.String(), `"status":"drafted"`) {
				t.Fatalf("commit failure leaked success: %d %s", w.Code, w.Body.String())
			}
			var drafts, children int
			if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1`, parent).Scan(&drafts); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, parent).Scan(&children); err != nil {
				t.Fatal(err)
			}
			want := 1
			if operation == "share" {
				want = 0
			}
			if drafts != want || children != 0 {
				t.Fatalf("rollback lost draft or created child: drafts=%d want=%d children=%d", drafts, want, children)
			}
		})
	}
}

func TestShortcutSourceResolutionNeverGuesses(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanapp := testdb.NewAsApp(t, schema)
	defer cleanapp()
	var owner, other int
	for name, id := range map[string]*int{"resolver-owner": &owner, "resolver-other": &other} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	first := seedShortcutSource(t, db, owner, "first", "[Exact](https://example.com/exact?utm_source=one#section) [Longer](https://example.com/longer)")
	second := seedShortcutSource(t, db, owner, "second", "[Exact again](https://example.com/exact/#other)")
	foreign := seedShortcutSource(t, db, other, "foreign", "[Only foreign](https://example.com/foreign)")
	hidden := seedShortcutSource(t, db, owner, "hidden", "[Hidden](https://example.com/hidden)")
	if _, err := db.Exec(`INSERT INTO hidden_articles(user_id,article_id) VALUES($1,$2)`, owner, hidden); err != nil {
		t.Fatal(err)
	}
	connections := repository.NewShortcutRepository(db)
	code, _, _ := connections.Pair(owner)
	token, err := connections.Connect(code)
	if err != nil {
		t.Fatal(err)
	}
	router := shortcutCaptureRouter(app, db)
	for _, tc := range []struct {
		name, url      string
		source, status int
	}{
		{"ambiguous", "https://example.com/exact", 0, 409},
		{"no-source", "https://example.com/missing", 0, 422},
		{"prefix-boundary", "https://example.com/long", 0, 422},
		{"foreign", "https://example.com/foreign", foreign, 422},
		{"hidden", "https://example.com/hidden", hidden, 422},
		{"explicit-missing-link", "https://example.com/missing", first, 422},
		{"explicit-disambiguation", "https://example.com/exact#share", second, 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/shortcuts/capture", strings.NewReader(fmt.Sprintf(`{"url":%q,"source_article_id":%d}`, tc.url, tc.source)))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
	var drafts int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts`).Scan(&drafts); err != nil || drafts != 1 {
		t.Fatalf("drafts=%d err=%v", drafts, err)
	}
}
