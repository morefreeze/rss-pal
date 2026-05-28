package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/rss-pal/internal/httpx"
)

// allowLoopbackValidator accepts loopback hosts. Used only in tests; never
// wired into production code.
func allowLoopbackValidator(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

// newTestProxy builds an ImageProxy with the given validator and a client
// whose CheckRedirect re-runs the same validator against redirect targets.
func newTestProxy(validator func(string) (*url.URL, error)) *ImageProxy {
	p := &ImageProxy{Validate: validator}
	p.Client = &http.Client{
		Timeout: proxyTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if _, err := p.Validate(req.URL.String()); err != nil {
				return fmt.Errorf("redirect rejected: %w", err)
			}
			return nil
		},
	}
	return p
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

	proxy := newTestProxy(allowLoopbackValidator)
	r := gin.New()
	r.GET("/api/proxy/image", proxy.Handle)

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

// TestProxyImage_OverridesUpstreamCacheControl pins the no-passthrough policy:
// upstream's anti-caching headers must not leak through, or mobile browsers
// re-fetch every image on scroll once they evict the decoded copy from memory.
func TestProxyImage_OverridesUpstreamCacheControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name           string
		upstreamHeader string
	}{
		{"upstream no-store", "no-store"},
		{"upstream no-cache", "no-cache"},
		{"upstream private", "private, max-age=0"},
		{"upstream must-revalidate", "max-age=0, must-revalidate"},
		{"upstream no header", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.upstreamHeader != "" {
					w.Header().Set("Cache-Control", tc.upstreamHeader)
				}
				w.Header().Set("Content-Type", "image/png")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nx"))
			}))
			defer origin.Close()

			proxy := newTestProxy(allowLoopbackValidator)
			r := gin.New()
			r.GET("/api/proxy/image", proxy.Handle)
			req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/x.png", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			got := rec.Header().Get("Cache-Control")
			want := "public, max-age=604800, immutable"
			if got != want {
				t.Errorf("Cache-Control = %q, want %q", got, want)
			}
		})
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

	proxy := newTestProxy(allowLoopbackValidator)
	r := gin.New()
	r.GET("/api/proxy/image", proxy.Handle)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/foo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 for text/html upstream, got 200")
	}
}

func TestProxyImage_RejectsBadScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proxy := newTestProxy(allowLoopbackValidator)
	r := gin.New()
	r.GET("/api/proxy/image", proxy.Handle)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url=file:///etc/passwd", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Errorf("expected 4xx for file:// scheme, got %d", rec.Code)
	}
}

func TestProxyImage_RejectsOversizeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "20971520") // 20 MB
		w.WriteHeader(http.StatusOK)
		// Body intentionally smaller than declared — handler should reject before reading.
		_, _ = w.Write([]byte("PNG_HEADER"))
	}))
	defer origin.Close()

	proxy := newTestProxy(allowLoopbackValidator)
	r := gin.New()
	r.GET("/api/proxy/image", proxy.Handle)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/big.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for oversize upstream, got %d", rec.Code)
	}
}

func TestProxyImage_RejectsRedirectToBlockedIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// origin redirects to a blocked metadata IP. The CheckRedirect hook in
	// the proxy's http.Client should re-run the SSRF guard and refuse.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer origin.Close()

	// Use the STRICT validator (validateImageURL). The first hop (origin's URL,
	// which is loopback) would normally be rejected too, so we need a validator
	// that allows loopback for the initial validation but is strict on redirect
	// targets. Use the production validator wrapped to bypass loopback for the
	// FIRST call only, then strict afterwards. Simplest approach: build a tiny
	// stateful wrapper for this test.
	calls := 0
	validator := func(raw string) (*url.URL, error) {
		calls++
		if calls == 1 {
			return allowLoopbackValidator(raw)
		}
		return httpx.ValidateURL(raw) // strict on redirects
	}
	proxy := newTestProxy(validator)
	r := gin.New()
	r.GET("/api/proxy/image", proxy.Handle)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/img.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// The redirect rejection in CheckRedirect surfaces as a transport error;
	// the handler turns that into 502.
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 after blocked redirect, got %d; body=%s", rec.Code, rec.Body.String())
	}
}
