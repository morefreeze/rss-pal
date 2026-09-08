package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

var shareAPINow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

type shareAPIFixture struct {
	privDB   *sql.DB
	appDB    *sql.DB
	router   *gin.Engine
	signer   *sharetoken.Signer
	now      time.Time
	article  int
	userA    int
	userB    int
	imageDir string
}

func newShareAPIFixture(t *testing.T) *shareAPIFixture {
	t.Helper()
	privDB, schema, cleanupSchema := testdb.NewWithSchema(t)
	appDB, cleanupApp := testdb.NewAsApp(t, schema)
	t.Cleanup(func() {
		cleanupApp()
		cleanupSchema()
	})

	f := &shareAPIFixture{privDB: privDB, appDB: appDB, now: shareAPINow}
	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('share-a', 'x') RETURNING id`).Scan(&f.userA); err != nil {
		t.Fatalf("seed user A: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('share-b', 'x') RETURNING id`).Scan(&f.userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	var feedID int
	if err := privDB.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('https://share.example/feed', 'Share Feed', $1) RETURNING id`, f.userA).Scan(&feedID); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if err := privDB.QueryRow(`
		INSERT INTO articles (feed_id, title, url, content, published_at, processing_state)
		VALUES ($1, 'Ready Article', 'https://share.example/article', 'shareable body', $2, 'ready')
		RETURNING id`, feedID, shareAPINow.Add(-time.Hour)).Scan(&f.article); err != nil {
		t.Fatalf("seed article: %v", err)
	}

	cfg := &config.Config{JWT: config.JWTConfig{Secret: "share-api-jwt-secret"}}
	auth := api.NewAuthHandler(cfg, repository.NewUserRepository(appDB), repository.NewRefreshTokenRepository(appDB))
	signer, err := sharetoken.NewSigner("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	f.signer = signer
	f.imageDir = t.TempDir()
	images := api.NewArticleImageHandler(f.imageDir, func(*gin.Context, int) (bool, error) { return true, nil })
	handler := api.NewShareHandler(repository.NewShareRepository(appDB), repository.NewArticleRepository(appDB), signer, images, func() time.Time { return f.now })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/share/:token", handler.GetPublic)
	router.GET("/api/share/:token/assets/:asset", handler.GetAsset)
	group := router.Group("/api")
	group.Use(auth.AuthMiddleware())
	group.Use(api.RLSTxMiddleware(appDB))
	group.POST("/articles/:id/shares", handler.Create)
	group.GET("/articles/:id/shares", handler.List)
	group.DELETE("/articles/:id/shares/:share_id", handler.Revoke)
	f.router = router
	return f
}

func (f *shareAPIFixture) publicRequest(method, path string) *httptest.ResponseRecorder {
	return f.publicRequestWithHeaders(method, path, nil)
}

func (f *shareAPIFixture) publicRequestWithHeaders(method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *shareAPIFixture) request(t *testing.T, method, path string, userID int, body string) *httptest.ResponseRecorder {
	t.Helper()
	claims := api.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("share-api-jwt-secret"))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+signed)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

type shareAPIResponse struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
	Legacy    bool       `json:"legacy"`
}

func (f *shareAPIFixture) articlePath(suffix string) string {
	return "/api/articles/" + strconv.Itoa(f.article) + suffix
}

func (f *shareAPIFixture) setArticleState(t *testing.T, state, content string) {
	t.Helper()
	if _, err := f.privDB.Exec(`UPDATE articles SET processing_state = $1, content = $2 WHERE id = $3`, state, content, f.article); err != nil {
		t.Fatalf("set article state: %v", err)
	}
}

func (f *shareAPIFixture) createShare(t *testing.T, userID int, body string) shareAPIResponse {
	t.Helper()
	w := f.request(t, http.MethodPost, f.articlePath("/shares"), userID, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create share status=%d body=%s", w.Code, w.Body.String())
	}
	assertShareResponseKeys(t, w.Body.Bytes())
	var response shareAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode share: %v", err)
	}
	if response.ID == "" || response.URL == "" || response.Status != "active" || response.CreatedAt.IsZero() {
		t.Fatalf("invalid create response: %+v", response)
	}
	token := strings.TrimPrefix(response.URL, "/share/")
	publicID, legacy, err := f.signer.Parse(token)
	if err != nil || legacy || publicID != response.ID {
		t.Fatalf("signed URL mismatch: id=%q url=%q parsed=%q legacy=%v err=%v", response.ID, response.URL, publicID, legacy, err)
	}
	return response
}

func assertShareResponseKeys(t *testing.T, raw []byte) {
	t.Helper()
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode response keys: %v", err)
	}
	want := map[string]bool{
		"id": true, "url": true, "created_at": true, "expires_at": true,
		"status": true, "legacy": true,
	}
	if len(got) != len(want) {
		t.Fatalf("response keys=%v, want exactly id,url,created_at,expires_at,status,legacy", got)
	}
	for key := range got {
		if !want[key] {
			t.Fatalf("response exposes forbidden key %q: %s", key, raw)
		}
	}
}

func assertPublicSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for key, want := range map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := w.Header().Get(key); got != want {
			t.Errorf("%s=%q, want %q", key, got, want)
		}
	}
}

func assertShareUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusNotFound || w.Body.String() != `{"error":"share unavailable"}` {
		t.Fatalf("status=%d body=%q, want generic 404", w.Code, w.Body.String())
	}
	assertPublicSecurityHeaders(t, w)
}

func TestPublicShareReturnsImmutableSnapshotAndRewritesOnlyExactAssets(t *testing.T) {
	f := newShareAPIFixture(t)
	content := `<p>snapshot</p><img src="/api/articles/123/images/0.png"><img src='/api/articles/456/images/9.jpeg'>` +
		`<img src="https://cdn.example/x.png"><code>/api/articles/123/images/2.gif</code>` +
		`<code>/prefix/api/articles/123/images/3.jpg</code><code>/api/articles/123/images/4.png?x=1</code>`
	f.setArticleState(t, "ready", content)
	share := f.createShare(t, f.userA, `{"expires_at":null}`)
	token := strings.TrimPrefix(share.URL, "/share/")
	f.setArticleState(t, "ready", "mutated article body")

	w := f.publicRequest(http.MethodGet, "/api/share/"+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertPublicSecurityHeaders(t, w)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode public response: %v", err)
	}
	wantKeys := map[string]bool{
		"title": true, "url": true, "feed_title": true, "published_at": true,
		"content": true, "snapshotted_at": true,
	}
	for key := range got {
		switch key {
		case "title", "url", "feed_title", "published_at", "word_count", "reading_minutes",
			"summary_brief", "summary_detailed", "content", "media_url", "media_type",
			"media_duration_seconds", "image_dimensions", "snapshotted_at":
		default:
			t.Fatalf("public response exposed forbidden key %q: %s", key, w.Body.String())
		}
	}
	for key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("public response missing key %q: %s", key, w.Body.String())
		}
	}
	var rewritten string
	if err := json.Unmarshal(got["content"], &rewritten); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewritten, "/api/share/"+token+"/assets/0.png") ||
		!strings.Contains(rewritten, "/api/share/"+token+"/assets/9.jpeg") {
		t.Fatalf("exact local assets not rewritten: %s", rewritten)
	}
	for _, unchanged := range []string{
		"https://cdn.example/x.png", "/api/articles/123/images/2.gif",
		"/prefix/api/articles/123/images/3.jpg", "/api/articles/123/images/4.png?x=1",
	} {
		if !strings.Contains(rewritten, unchanged) {
			t.Errorf("non-exact path %q was changed: %s", unchanged, rewritten)
		}
	}
	if strings.Contains(rewritten, "mutated article body") {
		t.Fatal("public response reread mutable article content")
	}
}

func TestPublicShareUnavailableIsUniform(t *testing.T) {
	f := newShareAPIFixture(t)
	active := f.createShare(t, f.userA, `{"expires_at":null}`)
	revoked := f.createShare(t, f.userA, `{"expires_at":null}`)
	expired := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T12:00:01Z"}`)
	if got := f.request(t, http.MethodDelete, f.articlePath("/shares/"+revoked.ID), f.userA, ""); got.Code != http.StatusOK {
		t.Fatalf("revoke status=%d", got.Code)
	}
	f.now = shareAPINow.Add(2 * time.Second)
	activeToken := strings.TrimPrefix(active.URL, "/share/")
	tampered := activeToken[:len(activeToken)-1] + "A"
	if tampered == activeToken {
		tampered = activeToken[:len(activeToken)-1] + "B"
	}
	for name, token := range map[string]string{
		"malformed": "not-a-token", "unknown": f.signer.Sign("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		"tampered": tampered, "revoked": strings.TrimPrefix(revoked.URL, "/share/"),
		"expired": strings.TrimPrefix(expired.URL, "/share/"), "legacy missing": "aB3dE6gH",
	} {
		t.Run(name, func(t *testing.T) {
			assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+token))
		})
	}
}

func TestPublicShareDatabaseFailureIsUnavailable(t *testing.T) {
	f := newShareAPIFixture(t)
	share := f.createShare(t, f.userA, `{"expires_at":null}`)
	if _, err := f.privDB.Exec(`DROP TABLE article_shares`); err != nil {
		t.Fatal(err)
	}
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+strings.TrimPrefix(share.URL, "/share/")))
}

func TestLegacyShareResolvesByDigestWithStrictExpiryBoundary(t *testing.T) {
	f := newShareAPIFixture(t)
	share := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T13:00:00Z"}`)
	const legacy = "aB3dE6gH"
	if _, err := f.privDB.Exec(`UPDATE article_shares SET legacy_token_digest = $1 WHERE public_id = $2`, sharetoken.LegacyDigest(legacy), share.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.publicRequest(http.MethodGet, "/api/share/"+legacy); got.Code != http.StatusOK {
		t.Fatalf("legacy active status=%d body=%s", got.Code, got.Body.String())
	}
	f.now = time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+legacy))
}

func TestShareAssetRequiresActiveShareAndPreservesAssetSemantics(t *testing.T) {
	f := newShareAPIFixture(t)
	share := f.createShare(t, f.userA, `{"expires_at":null}`)
	expiring := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T12:00:01Z"}`)
	token := strings.TrimPrefix(share.URL, "/share/")
	dir := filepath.Join(f.imageDir, "article_images", strconv.Itoa(f.article))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("shared-png")
	if err := os.WriteFile(filepath.Join(dir, "0.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	w := f.publicRequest(http.MethodGet, "/api/share/"+token+"/assets/0.png")
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.Bytes())
	}
	if w.Header().Get("ETag") == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("asset headers=%v", w.Header())
	}
	assertPublicSecurityHeaders(t, w)
	etag := w.Header().Get("ETag")
	revalidated := f.publicRequestWithHeaders(http.MethodGet, "/api/share/"+token+"/assets/0.png", map[string]string{"If-None-Match": etag})
	if revalidated.Code != http.StatusNotModified || revalidated.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("repeat asset status=%d", revalidated.Code)
	}

	for _, asset := range []string{"bad", "-1.png", "0.gif", "0.png%2F..%2F1.png"} {
		got := f.publicRequest(http.MethodGet, "/api/share/"+token+"/assets/"+asset)
		if got.Code < 400 {
			t.Errorf("unsafe asset %q status=%d", asset, got.Code)
		}
	}
	tampered := token[:len(token)-1] + "A"
	if tampered == token {
		tampered = token[:len(token)-1] + "B"
	}
	for name, invalidToken := range map[string]string{
		"tampered":       tampered,
		"legacy missing": "aB3dE6gH",
	} {
		t.Run(name, func(t *testing.T) {
			assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+invalidToken+"/assets/0.png"))
		})
	}
	f.now = shareAPINow.Add(2 * time.Second)
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+strings.TrimPrefix(expiring.URL, "/share/")+"/assets/0.png"))
	if got := f.request(t, http.MethodDelete, f.articlePath("/shares/"+share.ID), f.userA, ""); got.Code != http.StatusOK {
		t.Fatalf("revoke status=%d", got.Code)
	}
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/share/"+token+"/assets/0.png"))
}

func TestCreateShareRequiresVisibleReadyArticle(t *testing.T) {
	f := newShareAPIFixture(t)
	path := f.articlePath("/shares")
	if got := f.request(t, http.MethodPost, path, f.userA, `{"expires_at":null}`); got.Code != http.StatusCreated {
		t.Fatalf("owner status=%d body=%s", got.Code, got.Body.String())
	}
	if got := f.request(t, http.MethodPost, path, f.userB, `{"expires_at":null}`); got.Code != http.StatusNotFound {
		t.Fatalf("other status=%d body=%s", got.Code, got.Body.String())
	}
	f.setArticleState(t, "processing", "still fetching")
	if got := f.request(t, http.MethodPost, path, f.userA, `{"expires_at":null}`); got.Code != http.StatusConflict {
		t.Fatalf("processing status=%d body=%s", got.Code, got.Body.String())
	}
	f.setArticleState(t, "ready", " \n\t ")
	if got := f.request(t, http.MethodPost, path, f.userA, `{"expires_at":null}`); got.Code != http.StatusConflict {
		t.Fatalf("empty content status=%d body=%s", got.Code, got.Body.String())
	}
	f.setArticleState(t, "", "legacy ready body")
	if got := f.request(t, http.MethodPost, path, f.userA, `{"expires_at":null}`); got.Code != http.StatusCreated {
		t.Fatalf("empty processing_state status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestCreateShareValidatesExpiryAndCreatesMultipleRows(t *testing.T) {
	f := newShareAPIFixture(t)
	for name, body := range map[string]string{
		"missing":       `{}`,
		"misspelled":    `{"expiresAt":null}`,
		"unknown field": `{"expires_at":null,"unexpected":true}`,
		"trailing JSON": `{"expires_at":null}{"expires_at":null}`,
		"malformed":     `{"expires_at":"tomorrow"}`,
		"past":          `{"expires_at":"2026-09-08T11:59:59Z"}`,
		"equal":         `{"expires_at":"2026-09-08T12:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, body); got.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
		})
	}

	a := f.createShare(t, f.userA, `{"expires_at":null}`)
	if a.ExpiresAt != nil {
		t.Fatalf("explicit null must create a permanent share: %+v", a)
	}
	b := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T21:30:00+08:00"}`)
	if a.ID == b.ID || a.URL == b.URL {
		t.Fatalf("shares must be independent: a=%+v b=%+v", a, b)
	}
	if b.ExpiresAt == nil || !b.ExpiresAt.Equal(time.Date(2026, 9, 8, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("future expiry=%v", b.ExpiresAt)
	}
	var encoded map[string]json.RawMessage
	w := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":"2026-09-08T22:00:00+08:00"}`)
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &encoded) != nil || string(encoded["expires_at"]) != `"2026-09-08T14:00:00Z"` {
		t.Fatalf("offset expiry was not normalized to UTC: status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := f.privDB.QueryRow(`SELECT count(*) FROM article_shares WHERE article_id = $1`, f.article).Scan(&count); err != nil || count != 3 {
		t.Fatalf("persisted share count=%d err=%v", count, err)
	}
}

func TestCreateShareDoesNotLeakDatabaseErrors(t *testing.T) {
	f := newShareAPIFixture(t)
	if _, err := f.privDB.Exec(`DROP TABLE article_shares`); err != nil {
		t.Fatalf("drop share table: %v", err)
	}
	w := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":null}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"error":"internal server error"}` {
		t.Fatalf("database details leaked: %s", w.Body.String())
	}
}

func TestListSharesAndRevokeAreOwnerScoped(t *testing.T) {
	f := newShareAPIFixture(t)
	active := f.createShare(t, f.userA, `{"expires_at":null}`)
	expiring := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T13:00:00Z"}`)
	revoked := f.createShare(t, f.userA, `{"expires_at":null}`)
	if _, err := f.privDB.Exec(`UPDATE article_shares SET legacy_token_digest = $1 WHERE public_id = $2`, sharetoken.LegacyDigest("aB3dE6gH"), active.ID); err != nil {
		t.Fatalf("mark legacy row: %v", err)
	}

	if got := f.request(t, http.MethodGet, f.articlePath("/shares"), f.userB, ""); got.Code != http.StatusNotFound {
		t.Fatalf("other list status=%d body=%s", got.Code, got.Body.String())
	}
	revokePath := f.articlePath("/shares/" + revoked.ID)
	if got := f.request(t, http.MethodDelete, revokePath, f.userB, ""); got.Code != http.StatusNotFound {
		t.Fatalf("other revoke status=%d body=%s", got.Code, got.Body.String())
	}
	for i := 0; i < 2; i++ {
		got := f.request(t, http.MethodDelete, revokePath, f.userA, "")
		if got.Code != http.StatusOK {
			t.Fatalf("revoke attempt %d status=%d body=%s", i, got.Code, got.Body.String())
		}
		assertShareResponseKeys(t, got.Body.Bytes())
		var response shareAPIResponse
		if err := json.Unmarshal(got.Body.Bytes(), &response); err != nil || response.Status != "revoked" {
			t.Fatalf("revoke attempt %d response=%+v err=%v", i, response, err)
		}
	}

	f.now = shareAPINow.Add(2 * time.Hour)
	got := f.request(t, http.MethodGet, f.articlePath("/shares"), f.userA, "")
	if got.Code != http.StatusOK {
		t.Fatalf("owner list status=%d body=%s", got.Code, got.Body.String())
	}
	var rawRows []json.RawMessage
	if err := json.Unmarshal(got.Body.Bytes(), &rawRows); err != nil {
		t.Fatalf("decode list: %v body=%s", err, got.Body.String())
	}
	if len(rawRows) != 3 {
		t.Fatalf("list rows=%d body=%s", len(rawRows), got.Body.String())
	}
	statuses := make(map[string]shareAPIResponse)
	for _, raw := range rawRows {
		assertShareResponseKeys(t, raw)
		var response shareAPIResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("decode list row: %v", err)
		}
		statuses[response.ID] = response
	}
	if statuses[active.ID].Status != "active" || !statuses[active.ID].Legacy {
		t.Fatalf("active legacy row=%+v", statuses[active.ID])
	}
	if statuses[expiring.ID].Status != "expired" {
		t.Fatalf("expired row=%+v", statuses[expiring.ID])
	}
	if statuses[revoked.ID].Status != "revoked" {
		t.Fatalf("revoked row=%+v", statuses[revoked.ID])
	}
}

func TestRevokeShareHandlesMalformedAndMissingIDs(t *testing.T) {
	f := newShareAPIFixture(t)
	checks := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/api/articles/not-a-number/shares", http.StatusBadRequest},
		{http.MethodGet, "/api/articles/not-a-number/shares", http.StatusBadRequest},
		{http.MethodDelete, "/api/articles/not-a-number/shares/nope", http.StatusBadRequest},
		{http.MethodGet, "/api/articles/99999999/shares", http.StatusNotFound},
	}
	for _, check := range checks {
		body := ""
		if check.method == http.MethodPost {
			body = `{"expires_at":null}`
		}
		got := f.request(t, check.method, check.path, f.userA, body)
		if got.Code != check.want {
			t.Errorf("%s %s status=%d want=%d body=%s", check.method, check.path, got.Code, check.want, got.Body.String())
		}
	}

	if _, err := f.privDB.Exec(`DROP TABLE article_shares`); err != nil {
		t.Fatalf("drop share table: %v", err)
	}
	for _, shareID := range []string{
		"not-a-public-id",
		strings.Repeat("a", 31),
		strings.Repeat("a", 33),
		strings.Repeat("A", 32),
		strings.Repeat("a", 31) + "g",
	} {
		got := f.request(t, http.MethodDelete, f.articlePath("/shares/"+shareID), f.userA, "")
		if got.Code != http.StatusNotFound {
			t.Errorf("invalid share ID %q reached database: status=%d body=%s", shareID, got.Code, got.Body.String())
		}
	}
}
