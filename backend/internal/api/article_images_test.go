package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeArticleAccessCheckAlwaysOK always grants access; used by happy-path
// tests that don't care about the ownership rules.
func fakeArticleAccessCheckAlwaysOK(c *gin.Context, articleID int) (bool, error) {
	return true, nil
}

// fakeArticleAccessCheckAlwaysFail denies every request; exercises the 403
// branch without needing a real user/DB.
func fakeArticleAccessCheckAlwaysFail(c *gin.Context, articleID int) (bool, error) {
	return false, nil
}

func TestArticleImageHandler_200And304(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "article_images", "42")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("fake-png-bytes")
	if err := os.WriteFile(filepath.Join(dir, "3.png"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewArticleImageHandler(tmp, fakeArticleAccessCheckAlwaysOK)

	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)

	// First request: expect 200 with Cache-Control + ETag.
	req := httptest.NewRequest("GET", "/api/articles/42/images/3.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control: got %q", got)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type: got %q want image/png", ct)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header")
	}
	sum := sha256.Sum256(body)
	wantETag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if etag != wantETag {
		t.Errorf("ETag: got %s want %s", etag, wantETag)
	}
	if got := w.Body.Bytes(); string(got) != string(body) {
		t.Errorf("body mismatch: got %q want %q", got, body)
	}

	// Second request with matching If-None-Match: expect 304.
	req2 := httptest.NewRequest("GET", "/api/articles/42/images/3.png", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
}

func TestArticleImageHandler_JPEGContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "article_images", "7")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0.jpg"), []byte("fake-jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewArticleImageHandler(tmp, fakeArticleAccessCheckAlwaysOK)
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)

	req := httptest.NewRequest("GET", "/api/articles/7/images/0.jpg", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type: got %q want image/jpeg", ct)
	}
}

func TestArticleImageHandler_404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewArticleImageHandler(t.TempDir(), fakeArticleAccessCheckAlwaysOK)
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)
	req := httptest.NewRequest("GET", "/api/articles/99/images/0.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestArticleImageHandler_403_OtherUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "article_images", "42")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "0.png"), []byte("x"), 0o644)
	h := NewArticleImageHandler(tmp, fakeArticleAccessCheckAlwaysFail)
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)
	req := httptest.NewRequest("GET", "/api/articles/42/images/0.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestArticleImageHandler_500OnAccessInfraError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewArticleImageHandler(t.TempDir(), func(c *gin.Context, articleID int) (bool, error) {
		return false, os.ErrPermission // any non-nil error stands in for DB failure
	})
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)
	req := httptest.NewRequest("GET", "/api/articles/42/images/0.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on access infra error, got %d", w.Code)
	}
}

func TestArticleImageHandler_400OnMalformedIdx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewArticleImageHandler(t.TempDir(), fakeArticleAccessCheckAlwaysOK)
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)

	cases := []string{
		"/api/articles/42/images/bad",      // no dot
		"/api/articles/42/images/.png",     // empty index (dot at 0)
		"/api/articles/42/images/abc.png",  // index not numeric
		"/api/articles/42/images/5.",       // trailing dot (empty extension)
	}
	for _, path := range cases {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, w.Code)
		}
	}
}

func TestArticleImageHandler_400OnMalformedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewArticleImageHandler(t.TempDir(), fakeArticleAccessCheckAlwaysOK)
	r := gin.New()
	r.GET("/api/articles/:id/images/:idx", h.Serve)
	req := httptest.NewRequest("GET", "/api/articles/abc/images/0.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on non-int id, got %d", w.Code)
	}
}
