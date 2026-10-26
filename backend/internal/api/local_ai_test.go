package api

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerRejectsUserKeyStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &SettingsHandler{}
	r := gin.New()
	r.GET("/ai", h.GetAIConfig)
	r.PUT("/ai", h.SaveAIConfig)
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/ai", strings.NewReader(`{"api_key":"must-not-be-stored"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 410 || strings.Contains(w.Body.String(), "must-not-be-stored") {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
}
