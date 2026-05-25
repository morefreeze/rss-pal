package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gingzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// TestGzipMiddleware verifies the middleware compresses JSON responses
// over the min-content-length threshold when the client advertises
// Accept-Encoding: gzip.
func TestGzipMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gingzip.Gzip(gingzip.DefaultCompression))

	// Payload >512 bytes so it crosses the typical min-length threshold.
	payload := strings.Repeat("x", 2048)
	r.GET("/big", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": payload})
	})

	req := httptest.NewRequest(http.MethodGet, "/big", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", got)
	}
	if w.Body.Len() >= 2048 {
		t.Fatalf("expected compressed body smaller than raw, got %d bytes", w.Body.Len())
	}
}
