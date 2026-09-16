package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

func TestTaskBudgetBatchCostAndFailureRollback(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	policies, _ := taskbudget.LoadPolicies()
	p := policies["capture"]
	p.Daily = 3
	policies["capture"] = p
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", 1) }, TaskBudgetMiddleware(taskbudget.New(db), policies))
	called := 0
	r.POST("/api/extension/ingest", func(c *gin.Context) { called++; c.Status(500) })
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/extension/ingest", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	if got := request(`{"items":[{},{}]}`).Code; got != 500 {
		t.Fatalf("first %d", got)
	}
	if got := request(`{"items":[{},{}]}`).Code; got != 429 {
		t.Fatalf("batch bypassed quota: %d", got)
	}
	if called != 1 {
		t.Fatalf("upstream work called %d times", called)
	}
	if _, err := db.Exec(`DROP TABLE task_budget_daily`); err != nil {
		t.Fatal(err)
	}
	if got := request(`{"items":[{}]}`).Code; got != 503 {
		t.Fatalf("quota database failure: %d", got)
	}
}
func TestExpensiveTaskRouteCoverage(t *testing.T) {
	if taskRoute("GET", "/api/articles/:id/candidates") != "fetch" {
		t.Fatal("live candidate fetching must be metered")
	}
	for _, path := range []string{"/api/articles/:id/summary", "/api/settings/polish-prompt", "/api/feeds/:id/fetch", "/api/feeds/preview", "/api/bookmarklet/capture-pdf", "/api/bookmarklet/capture-pdf-url", "/api/bookmarklet/capture", "/api/extension/ingest", "/api/explore/sources/:id/subscribe", "/api/explore/sources/subscribe-batch", "/api/feeds/oneoff_link_set", "/api/interests/generate", "/api/insights/generate"} {
		if taskRoute("POST", path) == "" {
			t.Errorf("unmetered %s", path)
		}
	}
	if taskRoute("GET", "/api/articles/:id") != "" {
		t.Fatal("reading should not spend work quota")
	}
}
