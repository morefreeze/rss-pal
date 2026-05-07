package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Note: full DB-bound integration tests are deferred to manual verification
// (Task 13). These unit tests cover request parsing/validation only.

func TestPlaybackPut_BadID_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &PlaybackHandler{} // repo unused on this path
	r := gin.New()
	r.PUT("/api/articles/:id/playback", h.Put)

	body, _ := json.Marshal(map[string]any{"position_seconds": 10, "is_completed": false})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/articles/abc/playback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPlaybackGet_BadID_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &PlaybackHandler{}
	r := gin.New()
	r.GET("/api/articles/:id/playback", h.Get)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/articles/notanumber/playback", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPlaybackPut_InvalidJSON_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &PlaybackHandler{}
	r := gin.New()
	r.PUT("/api/articles/:id/playback", h.Put)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/articles/1/playback", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
