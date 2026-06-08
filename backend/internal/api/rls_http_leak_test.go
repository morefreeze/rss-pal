package api_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// httpLeakFixture stages two users + a private feed/article owned by userA
// behind a real router (AuthMiddleware + RLSTxMiddleware in front) whose
// DB pool is bound to the rsspal_app role. RLS therefore actually
// enforces — the bypass DSN option used by testdb.New() is NOT inherited
// by NewAsApp.
type httpLeakFixture struct {
	privDB                  *sql.DB
	appDB                   *sql.DB
	router                  *gin.Engine
	cfg                     *config.Config
	userA, userB            int
	privateFeedA, articleA  int
}

const httpTestJWTSecret = "test-secret-for-rls-http-leak"

func newHTTPLeakFixture(t *testing.T) (*httpLeakFixture, func()) {
	t.Helper()
	privDB, schema, cleanupSchema := testdb.NewWithSchema(t)
	appDB, cleanupApp := testdb.NewAsApp(t, schema)

	f := &httpLeakFixture{privDB: privDB, appDB: appDB}

	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('a', 'x') RETURNING id`).Scan(&f.userA); err != nil {
		t.Fatalf("seed userA: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('b', 'y') RETURNING id`).Scan(&f.userB); err != nil {
		t.Fatalf("seed userB: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('http://a', 'A', $1) RETURNING id`, f.userA).Scan(&f.privateFeedA); err != nil {
		t.Fatalf("seed feedA: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 'a1', 'http://a/1', NOW()) RETURNING id`, f.privateFeedA).Scan(&f.articleA); err != nil {
		t.Fatalf("seed articleA: %v", err)
	}

	cfg := &config.Config{JWT: config.JWTConfig{Secret: httpTestJWTSecret}}
	f.cfg = cfg

	// Build the minimal router under test. We wire only the endpoints in
	// scope; everything else would force pulling in summarizer/fetcher
	// machinery unrelated to RLS.
	feedRepo := repository.NewFeedRepository(appDB)
	articleRepo := repository.NewArticleRepository(appDB)
	progressRepo := repository.NewProgressRepository(appDB)
	prefRepo := repository.NewPreferenceRepository(appDB)
	hiddenRepo := repository.NewHiddenArticleRepository(appDB)
	articleUserTagRepo := repository.NewArticleUserTagRepository(appDB)

	feedHandler := api.NewFeedHandler(feedRepo, articleRepo, "")
	articleHandler := api.NewArticleHandler(
		articleRepo, articleUserTagRepo, progressRepo, prefRepo, hiddenRepo,
		nil, // summarizer — unused by GetByID/Hide
		nil, // contentFetcher — unused by GetByID/Hide
	)
	authHandler := api.NewAuthHandler(cfg, repository.NewUserRepository(appDB))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	apiGroup.Use(authHandler.AuthMiddleware())
	apiGroup.Use(api.RLSTxMiddleware(appDB))
	apiGroup.GET("/feeds/:id", feedHandler.GetByID)
	apiGroup.DELETE("/feeds/:id", feedHandler.Delete)
	apiGroup.GET("/articles/:id", articleHandler.GetByID)
	apiGroup.POST("/articles/:id/hide", articleHandler.Hide)
	f.router = r

	return f, func() {
		cleanupApp()
		cleanupSchema()
	}
}

func signTestJWT(t *testing.T, secret string, userID int, isAdmin bool) string {
	t.Helper()
	claims := api.Claims{
		UserID:   userID,
		Username: "u",
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func (f *httpLeakFixture) do(t *testing.T, method, path string, userID int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+signTestJWT(t, httpTestJWTSecret, userID, false))
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// ----------------------------------------------------------------------------
// Each test: userA owns the resource. userB targets it by id and must see
// 404 (the row is invisible under RLS), not 200 + a leaked body nor 403
// (which would suggest the row was found and then access-rejected).
// ----------------------------------------------------------------------------

func TestRLSHTTP_FeedGetByID_ScopedByOwner(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()

	// userA can see own feed.
	w := f.do(t, http.MethodGet, "/api/feeds/"+itoa(f.privateFeedA), f.userA)
	if w.Code != http.StatusOK {
		t.Fatalf("userA GET own feed: status %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// userB cannot — must be 404.
	w = f.do(t, http.MethodGet, "/api/feeds/"+itoa(f.privateFeedA), f.userB)
	if w.Code != http.StatusNotFound {
		t.Errorf("userB GET userA's feed: status %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestRLSHTTP_FeedDelete_ScopedByOwner(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()

	// userB cannot delete userA's feed: GetByID inside Delete must 404.
	w := f.do(t, http.MethodDelete, "/api/feeds/"+itoa(f.privateFeedA), f.userB)
	if w.Code != http.StatusNotFound {
		t.Errorf("userB DELETE userA's feed: status %d, want 404; body=%s", w.Code, w.Body.String())
	}

	// Confirm the row still exists (priv check — RLS bypassed).
	var n int
	if err := f.privDB.QueryRow(`SELECT COUNT(*) FROM feeds WHERE id = $1`, f.privateFeedA).Scan(&n); err != nil {
		t.Fatalf("post-check count: %v", err)
	}
	if n != 1 {
		t.Errorf("userA feed was deleted despite RLS isolation; remaining count=%d", n)
	}
}

func TestRLSHTTP_ArticleGetByID_ScopedByFeedOwner(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()

	// userA sees own article.
	w := f.do(t, http.MethodGet, "/api/articles/"+itoa(f.articleA), f.userA)
	if w.Code != http.StatusOK {
		t.Fatalf("userA GET own article: status %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// userB cannot see it.
	w = f.do(t, http.MethodGet, "/api/articles/"+itoa(f.articleA), f.userB)
	if w.Code != http.StatusNotFound {
		t.Errorf("userB GET userA's article: status %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestRLSHTTP_ArticleHide_ScopedByFeedOwner(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()

	// userB tries to hide userA's article — RLS hides the article from
	// userB's view so Hide's existence check 404s before any hidden_articles
	// row is written.
	w := f.do(t, http.MethodPost, "/api/articles/"+itoa(f.articleA)+"/hide", f.userB)
	if w.Code != http.StatusNotFound {
		t.Errorf("userB POST hide userA's article: status %d, want 404; body=%s", w.Code, w.Body.String())
	}

	// Confirm no hidden_articles row was written for userB.
	ctx := context.Background()
	var n int
	if err := f.privDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM hidden_articles WHERE article_id = $1 AND user_id = $2`, f.articleA, f.userB).Scan(&n); err != nil {
		t.Fatalf("count hidden: %v", err)
	}
	if n != 0 {
		t.Errorf("userB created hidden_articles row for userA's article (count=%d)", n)
	}
}

// itoa is a tiny local helper so we don't pull strconv just for two callsites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
