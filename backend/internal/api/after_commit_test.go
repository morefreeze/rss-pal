package api

import (
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestBackgroundWorkRunsOnlyAfterSuccessfulCommit(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", 1) }, RLSTxMiddleware(db))
	called := false
	r.POST("/ok", func(c *gin.Context) {
		tx := c.MustGet(CtxKeyTx)
		if tx == nil {
			t.Fatal("missing tx")
		}
		afterCommit(c, func() { called = true })
		if called {
			t.Error("background started before commit")
		}
		c.Status(202)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/ok", nil))
	if !called {
		t.Fatal("committed callback not run")
	}
	called = false
	r.POST("/fail", func(c *gin.Context) { afterCommit(c, func() { called = true }); c.Status(500) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/fail", nil))
	if called {
		t.Fatal("rolled back request started background work")
	}
}
