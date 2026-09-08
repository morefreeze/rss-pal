package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestShareRateLimiterLimitAndDownstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	calls := 0
	r := gin.New()
	r.GET("/share", NewShareRateLimiter(60, time.Minute, 4096, func() time.Time { return now }), func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	})
	for i := 0; i < 61; i++ {
		req := httptest.NewRequest(http.MethodGet, "/share", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := http.StatusNoContent
		if i == 60 {
			want = http.StatusTooManyRequests
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("429 missing Retry-After")
			}
		}
		if w.Code != want {
			t.Fatalf("request %d status=%d want=%d", i+1, w.Code, want)
		}
	}
	if calls != 60 {
		t.Fatalf("downstream calls=%d want=60", calls)
	}
}

func TestShareRateLimiterSeparatesIPsResetsAndBoundsClients(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	l := newShareRateLimiterState(1, time.Minute, 2, func() time.Time { return now })
	if !l.allow("192.0.2.1") || l.allow("192.0.2.1") {
		t.Fatal("first IP did not enforce per-window limit")
	}
	if !l.allow("192.0.2.2") {
		t.Fatal("second IP did not receive an independent window")
	}
	if !l.allow("192.0.2.3") || l.clientCount() != 2 {
		t.Fatalf("bounded map count=%d, want 2", l.clientCount())
	}
	now = now.Add(time.Minute)
	if !l.allow("192.0.2.1") {
		t.Fatal("window did not reset at boundary")
	}
}

func TestShareRateLimiterConcurrentAccess(t *testing.T) {
	l := newShareRateLimiterState(60, time.Minute, 16, time.Now)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 256; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("192.0.2.20") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 60 {
		t.Fatalf("concurrently allowed=%d want=60", got)
	}
}
