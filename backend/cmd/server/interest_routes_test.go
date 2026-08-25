package main

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func TestRegisterInterestRoutesIncludesCanonicalAndLegacyPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerInterestRoutes(router.Group("/api"), &api.InterestsHandler{})

	want := map[string]bool{
		"GET /api/interests/latest":    false,
		"POST /api/interests/generate": false,
		"GET /api/insights/latest":     false,
		"POST /api/insights/generate":  false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}
