package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

// TestGzipMiddleware verifies the middleware compresses JSON responses
// over the min-content-length threshold when the client advertises
// Accept-Encoding: gzip.
func TestGzipMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(api.ResponseCompression())

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

func TestResponseCompressionPreservesStreamAndAssetExclusions(t *testing.T) {
	for _, path := range []string{"/api/articles/7/summary/stream", "/api/media/youtube/video", "/asset.png"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(api.ResponseCompression())
			payload := strings.Repeat("uncompressed stream ", 128)
			router.GET(path, func(c *gin.Context) { c.String(http.StatusOK, payload) })
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept-Encoding", "gzip")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Header().Get("Content-Encoding") != "" || response.Body.String() != payload {
				t.Fatalf("excluded response was compressed: %v", response.Header())
			}
		})
	}
}
