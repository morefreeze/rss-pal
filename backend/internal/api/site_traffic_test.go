package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/opsmonitor"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestSiteTrafficAndAdminWindow(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userID int
	if err := db.QueryRow("INSERT INTO users(username,password_hash,is_admin) VALUES('traffic-admin','unused',true) RETURNING id").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/traffic/page-view", NewSiteTrafficHandler(db, "https://rss.example").Post)
	router.GET("/api/admin/monitoring", func(c *gin.Context) { c.Set("userID", userID) },
		NewAdminMonitoringHandler(db, opsmonitor.NewService(db, opsmonitor.DefaultConfig(), nil)).Get)
	post := func(origin, fetchSite, body string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/traffic/page-view", strings.NewReader(body))
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", fetchSite)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("post: got %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	const first = `{"event":"12345678-1234-4234-a234-123456789010","visitor":"12345678-1234-4234-a234-123456789020","source":"campaign"}`
	const second = `{"event":"12345678-1234-4234-a234-123456789011","visitor":"12345678-1234-4234-a234-123456789020","source":"internal"}`
	const third = `{"event":"12345678-1234-4234-a234-123456789012","visitor":"12345678-1234-4234-a234-123456789021","source":"direct"}`
	post("https://other.example", "cross-site", first, 403)
	post("https://rss.example", "same-origin", strings.Replace(first, "campaign", "private.example", 1), 400)
	post("https://rss.example", "same-origin", first+first, 400)
	post("https://rss.example", "same-origin", first, 204)
	post("https://rss.example", "same-origin", first, 204)
	post("https://rss.example", "same-origin", second, 204)
	post("https://rss.example", "same-origin", third, 204)
	if _, err := db.Exec("UPDATE site_page_views SET at=at-interval '1 hour' WHERE event_id=$1", "12345678-1234-4234-a234-123456789010"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/monitoring?hours=3", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("admin status %d: %s", w.Code, w.Body.String())
	}
	var result opsmonitor.Response
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Traffic == nil || result.Traffic.PageViews != 3 || result.Traffic.Visitors != 2 || !result.Traffic.Partial {
		t.Fatalf("incorrect traffic window: %+v", result.Traffic)
	}
	if len(result.Traffic.TimeSeries) != 2 || len(result.Traffic.BySource) != 3 || result.Traffic.BySource[0].Source != "campaign" || result.NewUsers != 1 {
		t.Fatalf("unexpected trend, source or new users: %s", w.Body.String())
	}
}
