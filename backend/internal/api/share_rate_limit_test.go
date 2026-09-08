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

func TestPublicShareRateLimitersHaveIndependentProductionBudgets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pageLimit, assetLimit := NewPublicShareRateLimiters(func() time.Time { return now })
	r := gin.New()
	r.GET("/page", pageLimit, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/asset", assetLimit, func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.30:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if got := request("/page").Code; got != http.StatusNoContent {
		t.Fatalf("initial page status=%d", got)
	}
	for i := 0; i < 240; i++ {
		if got := request("/asset").Code; got != http.StatusNoContent {
			t.Fatalf("asset request %d status=%d", i+1, got)
		}
	}
	if got := request("/asset"); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
		t.Fatalf("asset request 241 status=%d headers=%v", got.Code, got.Header())
	}
	for i := 1; i < 60; i++ {
		if got := request("/page").Code; got != http.StatusNoContent {
			t.Fatalf("page request %d status=%d; asset budget leaked into page state", i+1, got)
		}
	}
	if got := request("/page").Code; got != http.StatusTooManyRequests {
		t.Fatalf("page request 61 status=%d", got)
	}
}

func TestShareRateLimiterClientIPHonorsTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	r.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	tests := []struct {
		name, remote, xff, want string
	}{
		{
			name:   "trusted nginx appends actual client after spoofed value",
			remote: "10.0.0.8:1234", xff: "198.51.100.66, 203.0.113.44", want: "203.0.113.44",
		},
		{
			name:   "direct untrusted peer cannot spoof with xff",
			remote: "203.0.113.44:1234", xff: "198.51.100.66", want: "203.0.113.44",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = test.remote
			req.Header.Set("X-Forwarded-For", test.xff)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if got := w.Body.String(); got != test.want {
				t.Fatalf("ClientIP=%q want=%q", got, test.want)
			}
		})
	}
}
