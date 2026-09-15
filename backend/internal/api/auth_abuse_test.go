package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type memoryAuthBudget struct {
	mu     sync.Mutex
	counts map[string]int
	fail   bool
}

func (m *memoryAuthBudget) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return false, 0, errors.New("db unavailable with sensitive details")
	}
	if m.counts == nil {
		m.counts = map[string]int{}
	}
	m.counts[key]++
	return m.counts[key] <= limit, window, nil
}
func abuseRequest(r http.Handler, path, body, remote, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", xff)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestAuthAbuseBlocksAccountAcrossIPsAndForgedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memoryAuthBudget{}
	guard := NewAuthAbuseGuard(store, "unit-test-key")
	r := gin.New()
	_ = r.SetTrustedProxies([]string{"127.0.0.1"})
	r.POST("/login", guard.Middleware("login"), func(c *gin.Context) { c.Status(204) })
	for i := 0; i < 10; i++ {
		w := abuseRequest(r, "/login", `{"username":"alice","password":"bad"}`, "198.51.100.1:1234", "203.0.113.3")
		if w.Code != 204 {
			t.Fatalf("attempt %d = %d", i, w.Code)
		}
	}
	w := abuseRequest(r, "/login", `{"username":"ALICE","password":"bad"}`, "198.51.100.2:1234", "203.0.113.4")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("account limit = %d %s", w.Code, w.Body.String())
	}
	for key := range store.counts {
		if len(key) != 64 || strings.Contains(key, "alice") {
			t.Fatalf("identifier not hashed: %q", key)
		}
	}
}
func TestAuthAbuseIPv6PrefixAndMappedIPv4(t *testing.T) {
	for _, pair := range [][2]string{{"2001:db8:1:2::1", "2001:db8:1:2::ffff"}, {"192.0.2.1", "::ffff:192.0.2.1"}} {
		if authClientNetwork(pair[0]) != authClientNetwork(pair[1]) {
			t.Fatalf("different budgets for %v", pair)
		}
	}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	guard := NewAuthAbuseGuard(&memoryAuthBudget{}, "test-key")
	r.POST("/register", guard.Middleware("register"), func(c *gin.Context) { c.Status(204) })
	for i := 0; i < 5; i++ {
		if w := abuseRequest(r, "/register", `{}`, "[2001:db8:1:2::1]:1234", ""); w.Code != 204 {
			t.Fatal(w.Code)
		}
	}
	if w := abuseRequest(r, "/register", `{}`, "[2001:db8:1:2::999]:1234", ""); w.Code != 429 {
		t.Fatalf("IPv6 rotation bypassed: %d", w.Code)
	}
}
func TestAuthAbuseRejectsOversizeMalformedAndUnavailable(t *testing.T) {
	store := &memoryAuthBudget{}
	guard := NewAuthAbuseGuard(store, "test-key")
	r := gin.New()
	r.POST("/login", guard.Middleware("login"), func(c *gin.Context) { c.Status(204) })
	cases := []struct {
		body string
		code int
	}{{strings.Repeat("x", 16385), 413}, {`{"username":`, 400}, {`{"username":3}`, 400}, {`{} {}`, 400}}
	for _, tc := range cases {
		if w := abuseRequest(r, "/login", tc.body, "192.0.2.1:1234", ""); w.Code != tc.code {
			t.Fatalf("wanted %d got %d", tc.code, w.Code)
		}
	}
	store.fail = true
	w := abuseRequest(r, "/login", `{"username":"alice"}`, "192.0.2.1:1234", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatalf("failure did not close safely: %d %s", w.Code, w.Body.String())
	}
}
func TestAuthAbusePreservesJSONAndRejectsForgedIPRotation(t *testing.T) {
	guard := NewAuthAbuseGuard(&memoryAuthBudget{}, "test-key")
	r := gin.New()
	_ = r.SetTrustedProxies([]string{"127.0.0.1"})
	r.POST("/register", guard.Middleware("register"), func(c *gin.Context) {
		var b struct {
			Code string `json:"code"`
		}
		if c.ShouldBindJSON(&b) != nil || b.Code != "invite" {
			t.Error("body lost")
		}
		c.Status(204)
	})
	for i := 0; i < 6; i++ {
		w := abuseRequest(r, "/register", `{"code":"invite"}`, "192.0.2.1:1234", "1.2.3."+string(rune('1'+i)))
		want := 204
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("request %d got %d", i, w.Code)
		}
	}
}

func TestAuthAbuseRejectedIPDoesNotExhaustOtherClients(t *testing.T) {
	guard := NewAuthAbuseGuard(&memoryAuthBudget{}, "key")
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.POST("/login", guard.Middleware("login"), func(c *gin.Context) { c.Status(204) })
	for i := 0; i < 650; i++ {
		abuseRequest(r, "/login", `{"username":"attacker"}`, "192.0.2.1:1", "")
	}
	w := abuseRequest(r, "/login", `{"username":"legitimate"}`, "192.0.2.2:1", "")
	if w.Code != 204 {
		t.Fatalf("blocked client exhausted global budget: %d", w.Code)
	}
}
