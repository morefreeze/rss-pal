package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Validation-only tests. DB-bound integration deferred to manual verification.

func TestEventPost_BadJSON_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEventPost_InvalidEventType_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	body, _ := json.Marshal(map[string]any{"article_id": 1, "event_type": "lol"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEventPost_MissingArticleID_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	body, _ := json.Marshal(map[string]any{"event_type": "click"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
