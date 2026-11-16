package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestPreviewRoutesRequireCurrentAdminRole(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userID int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('reader', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api", func(c *gin.Context) {
		c.Set("userID", userID)
		// A token carrying the old role must not grant access after demotion.
		c.Set("isAdmin", true)
	})
	guard := api.AdminPreviewOnly(db)
	registerPreviewRoutes(group.Group("", guard), &api.ExploreHandler{}, &api.DailyHandler{}, &api.WeeklyHandler{}, &api.BriefingHandler{}, &api.BriefingIndexHandler{})
	group.GET("/role-check", guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })

	routes := []string{
		"GET /api/explore",
		"GET /api/explore/sources",
		"GET /api/explore/articles/1",
		"POST /api/explore/articles/1/content",
		"PUT /api/explore/articles/1/state",
		"POST /api/explore/feedback",
		"DELETE /api/explore/feedback",
		"DELETE /api/explore/feedback/1",
		"PUT /api/explore/interests",
		"POST /api/explore/articles/1/events",
		"POST /api/explore/sources/subscribe-batch",
		"POST /api/explore/sources/1/subscribe",
		"GET /api/weekly-digest",
		"GET /api/daily-digest",
		"GET /api/briefing/last-tab",
		"POST /api/briefing/last-tab",
		"GET /api/briefing/index",
	}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			method, path, _ := strings.Cut(route, " ")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusNotFound || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("ordinary account: %d %q, headers %v", w.Code, w.Body.String(), w.Header())
			}
		})
	}
	if _, err := db.Exec(`UPDATE users SET is_admin=true WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/role-check", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("current administrator did not reach handler: %d", w.Code)
	}
}
