// Package httpx provides an SSRF-guarded HTTP client + URL validator shared
// by image proxy + imagefetch (vision summary input). All callers that fetch
// arbitrary external image URLs must go through ValidateURL + the client
// returned by NewClient so we don't accidentally serve cloud metadata or
// internal services to attackers.
package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// blockedCIDRs is the IPv4/IPv6 ranges we refuse to talk to: loopback,
// RFC1918, link-local (covers AWS/GCP/Azure metadata 169.254.169.254),
// IPv6 ULA, IPv6 link-local.
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

// ValidateURL parses raw, requires http/https, and rejects hosts whose
// resolved IPs land in any blocked range. Returns the parsed URL on success.
func ValidateURL(raw string) (*url.URL, error) {
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
			return nil, errors.New("blocked address")
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, errors.New("blocked address")
		}
	}
	return u, nil
}

// UserAgent is the User-Agent string used by both the image proxy and the
// imagefetch downloader. Mirrors a Chrome desktop UA so hotlink-protected
// CDNs (WeChat, Zhihu) don't 403.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// NewClient returns a client that re-validates redirect targets against the
// SSRF guard. timeout caps the full request-to-response duration.
func NewClient(timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if _, err := ValidateURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect rejected: %w", err)
		}
		return nil
	}
	return c
}
