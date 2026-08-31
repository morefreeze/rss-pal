package main

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func TestRegisterExploreRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerExploreRoutes(router.Group("/api"), &api.ExploreHandler{})
	want := map[string]bool{
		"GET /api/explore":                          false,
		"GET /api/explore/sources":                  false,
		"GET /api/explore/articles/:id":             false,
		"POST /api/explore/feedback":                false,
		"DELETE /api/explore/feedback/:id":          false,
		"DELETE /api/explore/feedback":              false,
		"PUT /api/explore/interests":                false,
		"POST /api/explore/articles/:id/events":     false,
		"POST /api/explore/sources/:id/subscribe":   false,
		"POST /api/explore/sources/subscribe-batch": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, registered := range want {
		if !registered {
			t.Errorf("missing protected Explore route %s", route)
		}
	}
}
