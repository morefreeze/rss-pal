package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // metadata
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("parse %q failed", tc.ip)
		}
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateImageURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/img.png", false},
		{"http://example.com/img.png", false},
		{"ftp://example.com/img.png", true},
		{"file:///etc/passwd", true},
		{"https://127.0.0.1/img.png", true},
		{"https://192.168.1.1/img.png", true},
		{"not-a-url", true},
		{"", true},
	}
	for _, tc := range cases {
		_, err := validateImageURL(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateImageURL(%q) err=%v, wantErr=%v", tc.raw, err, tc.wantErr)
		}
	}
}

func TestProxyImage_StreamsAndInjectsReferer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotReferer string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nFAKEBYTES"))
	}))
	defer origin.Close()

	allowLoopbackForTesting = true
	t.Cleanup(func() { allowLoopbackForTesting = false })

	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/cat.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if !strings.HasPrefix(gotReferer, origin.URL) {
		t.Errorf("referer = %q, want prefix %q", gotReferer, origin.URL)
	}
	if !strings.Contains(rec.Body.String(), "FAKEBYTES") {
		t.Errorf("body did not stream upstream payload: %q", rec.Body.String())
	}
}

func TestProxyImage_RejectsNonImageContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>"))
	}))
	defer origin.Close()

	allowLoopbackForTesting = true
	t.Cleanup(func() { allowLoopbackForTesting = false })

	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/foo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 for text/html upstream, got 200")
	}
}

func TestProxyImage_RejectsBadScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url=file:///etc/passwd", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Errorf("expected 4xx for file:// scheme, got %d", rec.Code)
	}
}
