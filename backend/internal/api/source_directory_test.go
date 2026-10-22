package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/sourcedirectory"
	"github.com/gin-gonic/gin"
)

func TestSourceDirectoryServesEmbeddedCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := NewAuthHandler(&config.Config{}, nil, nil)
	group := router.Group("/api", auth.AuthMiddleware())
	group.GET("/source-directory", NewSourceDirectoryHandler(sourcedirectory.New()).Get)
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/source-directory", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatal("directory route must require authentication")
	}
	token, err := auth.signToken(1, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/source-directory?upstream=http://127.0.0.1", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var d sourcedirectory.Directory
	if err := json.Unmarshal(response.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.PlatformCount < 2067 || d.RouteCount < 3955 {
		t.Fatal(d.PlatformCount, d.RouteCount)
	}
}
