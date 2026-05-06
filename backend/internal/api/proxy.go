package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// allowLoopbackForTesting, when true, lets validateImageURL accept loopback
// targets. Only set from _test.go files. Used to allow httptest.NewServer
// URLs (which resolve to 127.0.0.1) in test scenarios.
var allowLoopbackForTesting bool

// blockedCIDRs is the IPv4/IPv6 ranges we refuse to proxy to. Covers loopback,
// RFC1918 private ranges, link-local, IPv6 ULA, and the cloud metadata IP.
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	out := make([]*net.IPNet, 0, len(raw))
	for _, c := range raw {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("bad cidr %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}()

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validateImageURL parses raw, requires http/https, and rejects hosts whose
// resolved IPs land in any blocked range. Returns the parsed URL on success.
func validateImageURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			if allowLoopbackForTesting && ip.IsLoopback() {
				// allow — test path
			} else {
				return nil, errors.New("blocked address")
			}
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			if allowLoopbackForTesting && ip.IsLoopback() {
				// allow — test path
			} else {
				return nil, errors.New("blocked address")
			}
		}
	}
	return u, nil
}

const (
	proxyMaxBytes  = 10 * 1024 * 1024 // 10MB
	proxyTimeout   = 30 * time.Second
	proxyUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var proxyClient = &http.Client{Timeout: proxyTimeout}

// ProxyImage streams an image from a remote URL through this server. It is
// unauthenticated by design — image tags do not carry our auth cookie/JWT
// reliably and the content is public anyway. SSRF is the real risk and is
// handled by validateImageURL.
func ProxyImage(c *gin.Context) {
	raw := c.Query("url")
	target, err := validateImageURL(raw)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid url: %s", err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), proxyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.String(http.StatusBadRequest, "build request: %s", err)
		return
	}
	// Inject a Referer matching the target origin to defeat hotlink protection
	// (notably WeChat / Zhihu).
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	req.Header.Set("User-Agent", proxyUserAgent)

	resp, err := proxyClient.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "upstream: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.String(http.StatusBadGateway, "upstream status %d", resp.StatusCode)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		c.String(http.StatusUnsupportedMediaType, "non-image content-type: %s", ct)
		return
	}

	c.Header("Content-Type", ct)
	if et := resp.Header.Get("ETag"); et != "" {
		c.Header("ETag", et)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		c.Header("Cache-Control", cc)
	} else {
		c.Header("Cache-Control", "public, max-age=86400, immutable")
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, proxyMaxBytes))
}
