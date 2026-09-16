package api

import (
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTencentRegistrationConfig(t *testing.T) {
	for _, tc := range []struct {
		id, secret, provider string
		available            bool
	}{
		{"123", "private-secret", "tencent", true}, {"", "private-secret", "tencent", false},
		{"18446744073709551616", "private-secret", "tencent", false}, {"123", " ", "tencent", false},
		{"123", "private-secret", "unknown", false},
	} {
		cfg := &config.Config{Auth: config.AuthConfig{CaptchaProvider: tc.provider, TencentCaptchaAppID: tc.id, TencentCaptchaAppSecret: tc.secret, TencentSecretID: tc.secret, TencentSecretKey: tc.secret}}
		h := NewAuthHandler(cfg, nil, nil)
		r := gin.New()
		r.GET("/config", h.RegistrationConfig)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/config", nil))
		var body struct {
			Available bool
			Provider  string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Available != tc.available || body.Provider != tc.provider || strings.Contains(w.Body.String(), "private-secret") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%+v: %s", tc, w.Body.String())
		}
	}
}
