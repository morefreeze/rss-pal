package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestRLSMiddleware_SetsUserID(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", 42); c.Next() })
	r.Use(api.RLSTxMiddleware(db))
	r.GET("/check", func(c *gin.Context) {
		q := c.MustGet(api.CtxKeyTx).(repository.Querier)
		var got string
		if err := q.QueryRow(`SELECT current_setting('app.user_id', true)`).Scan(&got); err != nil {
			t.Fatalf("scan: %v", err)
			return
		}
		if got != "42" {
			t.Fatalf("expected app.user_id=42, got %q", got)
		}
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
}
