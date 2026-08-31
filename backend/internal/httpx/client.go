// Package httpx provides an SSRF-guarded HTTP client + URL validator shared
// by image proxy + imagefetch (vision summary input). All callers that fetch
// arbitrary external image URLs must go through ValidateURL + the client
// returned by NewClient so we don't accidentally serve cloud metadata or
// internal services to attackers.
package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

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
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

var blockedPorts = map[uint16]struct{}{
	21: {}, 22: {}, 23: {}, 25: {}, 53: {}, 110: {}, 135: {}, 139: {}, 143: {},
	389: {}, 445: {}, 465: {}, 587: {}, 1433: {}, 1521: {}, 2375: {}, 2376: {},
	3306: {}, 5432: {}, 5672: {}, 6379: {}, 9200: {}, 11211: {}, 27017: {},
}

// ValidateURL parses raw, requires http/https, and rejects hosts whose
// resolved IPs land in any blocked range. Returns the parsed URL on success.
func ValidateURL(raw string) (*url.URL, error) {
	return validateURL(context.Background(), raw, net.DefaultResolver)
}

func validateURL(ctx context.Context, raw string, resolver resolver) (*url.URL, error) {
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
	if u.User != nil {
		return nil, errors.New("credentials are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("missing host")
	}
	if port := u.Port(); port != "" {
		if err := validatePort(port); err != nil {
			return nil, err
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, errors.New("blocked address")
		}
		return u, nil
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("dns: no addresses")
	}
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			return nil, errors.New("blocked address")
		}
	}
	return u, nil
}

func validatePort(port string) error {
	parsed, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsed == 0 {
		return errors.New("invalid port")
	}
	if _, blocked := blockedPorts[uint16(parsed)]; blocked {
		return errors.New("blocked port")
	}
	return nil
}

// UserAgent is the User-Agent string used by both the image proxy and the
// imagefetch downloader. Mirrors a Chrome desktop UA so hotlink-protected
// CDNs (WeChat, Zhihu) don't 403.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var ErrResponseTooLarge = errors.New("response exceeds configured limit")

// FetchResult is the bounded response returned by FetchBounded. Header is a
// copy of the received response headers and Body contains at most the supplied
// byte limit.
type FetchResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	// FinalURL is the response request URL after redirects. It lets callers
	// resolve relative document references without weakening redirect checks.
	FinalURL string
}

// FetchBounded validates and fetches a public URL with an explicitly supplied
// client. It reads at most limit bytes, treating every non-2xx status as an
// error while still returning the response metadata and body to the caller.
func FetchBounded(ctx context.Context, client *http.Client, rawURL string, headers http.Header, limit int64) (*FetchResult, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	if limit <= 0 {
		return nil, errors.New("response limit must be positive")
	}
	if limit == int64(^uint64(0)>>1) {
		return nil, errors.New("response limit is too large")
	}
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	requestClient := *client
	previousRedirectCheck := client.CheckRedirect
	requestClient.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if _, err := ValidateURL(redirect.URL.String()); err != nil {
			return fmt.Errorf("redirect rejected: %w", err)
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(redirect, via)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	response, err := requestClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	finalURL := u.String()
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	result := &FetchResult{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: body, FinalURL: finalURL}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	return result, nil
}

// NewClient returns a client that re-validates redirect targets against the
// SSRF guard. timeout caps the full request-to-response duration.
func NewClient(timeout time.Duration) *http.Client {
	return newClientWithProxy(timeout, net.DefaultResolver, &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}, http.ProxyFromEnvironment)
}

func newClient(timeout time.Duration, resolver resolver, dialer dialer) *http.Client {
	return newClientWithProxy(timeout, resolver, dialer, nil)
}

func newClientWithProxy(timeout time.Duration, resolver resolver, dialer dialer, proxy func(*http.Request) (*url.URL, error)) *http.Client {
	return newClientWithProxyAndTLS(timeout, resolver, dialer, proxy, nil)
}

func newClientWithProxyAndTLS(timeout time.Duration, resolver resolver, dialer dialer, proxy func(*http.Request) (*url.URL, error), tlsConfig *tls.Config) *http.Client {
	transport := &http.Transport{
		DialContext:           safeDialContext(resolver, dialer),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   boundedTimeout(timeout, 10*time.Second),
		ResponseHeaderTimeout: boundedTimeout(timeout, 15*time.Second),
		ExpectContinueTimeout: time.Second,
	}
	c := &http.Client{Timeout: timeout, Transport: &safeRoundTripper{direct: transport, resolver: resolver, dialer: dialer, proxy: proxy, tlsConfig: tlsConfig}}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if _, err := validateURL(req.Context(), req.URL.String(), resolver); err != nil {
			return fmt.Errorf("redirect rejected: %w", err)
		}
		return nil
	}
	return c
}

// safeRoundTripper pins every outbound target to a checked IP. This is needed
// even with a proxy: the proxy receives only the checked IP as its HTTP target
// or CONNECT authority, while the original host remains the HTTP Host header
// and HTTPS SNI value.
type safeRoundTripper struct {
	direct    *http.Transport
	resolver  resolver
	dialer    dialer
	proxy     func(*http.Request) (*url.URL, error)
	tlsConfig *tls.Config
}

func (t *safeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := validateURL(req.Context(), req.URL.String(), t.resolver); err != nil {
		return nil, err
	}

	var proxyURL *url.URL
	var err error
	if t.proxy != nil {
		proxyURL, err = t.proxy(req)
		if err != nil {
			return nil, err
		}
	}
	if proxyURL != nil {
		pinned, serverName, err := pinRequest(req, t.resolver)
		if err != nil {
			return nil, err
		}
		if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
			return roundTripViaSOCKSProxy(t.direct, t.dialer, proxyURL, pinned, serverName, t.tlsConfig)
		}
		return roundTripViaHTTPProxy(req.Context(), t.dialer, proxyURL, pinned, serverName, t.tlsConfig)
	}
	return t.direct.RoundTrip(req)
}

func roundTripViaHTTPProxy(ctx context.Context, dialer dialer, proxyURL *url.URL, req *http.Request, serverName string, tlsConfig *tls.Config) (*http.Response, error) {
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddress(proxyURL))
	if err != nil {
		return nil, err
	}
	rawConn := conn
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = rawConn.Close()
		case <-done:
		}
	}()
	success := false
	defer func() {
		if !success {
			close(done)
			_ = rawConn.Close()
		}
	}()
	reader := bufio.NewReader(conn)
	if proxyURL.Scheme == "https" {
		proxyTLS := tls.Client(conn, tlsConfigForServerName(tlsConfig, proxyURL.Hostname()))
		if err := proxyTLS.HandshakeContext(ctx); err != nil {
			return nil, contextError(ctx, err)
		}
		conn = proxyTLS
		reader = bufio.NewReader(conn)
	}
	if req.URL.Scheme == "https" {
		if err := writeConnectRequest(conn, proxyURL, req.URL.Host); err != nil {
			conn.Close()
			return nil, err
		}
		connectResponse, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			conn.Close()
			return nil, contextError(ctx, err)
		}
		if connectResponse.StatusCode < http.StatusOK || connectResponse.StatusCode >= http.StatusMultipleChoices {
			connectResponse.Body.Close()
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT returned HTTP status %d", connectResponse.StatusCode)
		}
		connectResponse.Body.Close()
		tlsConn := tls.Client(conn, tlsConfigForServerName(tlsConfig, serverName))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, contextError(ctx, err)
		}
		conn = tlsConn
		reader = bufio.NewReader(conn)
		originRequest := req.Clone(ctx)
		originRequest.URL.Scheme = ""
		originRequest.URL.Host = ""
		if err := originRequest.Write(conn); err != nil {
			conn.Close()
			return nil, contextError(ctx, err)
		}
	} else if err := writeProxyRequest(conn, proxyURL, req); err != nil {
		conn.Close()
		return nil, contextError(ctx, err)
	}

	response, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, contextError(ctx, err)
	}
	response.Request = req
	response.Body = &closeOnClose{ReadCloser: response.Body, conn: conn, done: done}
	success = true
	return response, nil
}

func roundTripViaSOCKSProxy(base *http.Transport, dialer dialer, proxyURL *url.URL, req *http.Request, serverName string, tlsConfig *tls.Config) (*http.Response, error) {
	transport := base.Clone()
	transport.Proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	// The configured proxy hop is operator-trusted; the SOCKS target is the
	// pinned IP in req.URL, so it cannot resolve a rebinding hostname itself.
	transport.DialContext = dialer.DialContext
	if req.URL.Scheme == "https" {
		transport.TLSClientConfig = tlsConfigForServerName(tlsConfig, serverName)
	}
	return transport.RoundTrip(req)
}

func proxyAddress(proxyURL *url.URL) string {
	if _, _, err := net.SplitHostPort(proxyURL.Host); err == nil {
		return proxyURL.Host
	}
	port := "80"
	if proxyURL.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func writeConnectRequest(w io.Writer, proxyURL *url.URL, target string) error {
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	setProxyAuthorization(request.Header, proxyURL)
	return request.Write(w)
}

func writeProxyRequest(w io.Writer, proxyURL *url.URL, req *http.Request) error {
	writer := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(writer, "%s %s HTTP/1.1\r\n", req.Method, req.URL.String()); err != nil {
		return err
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if _, err := fmt.Fprintf(writer, "Host: %s\r\n", host); err != nil {
		return err
	}
	headers := req.Header.Clone()
	headers.Del("Host")
	setProxyAuthorization(headers, proxyURL)
	if req.ContentLength > 0 && headers.Get("Content-Length") == "" {
		headers.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	}
	if err := headers.Write(writer); err != nil {
		return err
	}
	if _, err := writer.WriteString("\r\n"); err != nil {
		return err
	}
	if req.Body != nil {
		if _, err := io.Copy(writer, req.Body); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func setProxyAuthorization(headers http.Header, proxyURL *url.URL) {
	if proxyURL.User == nil {
		return
	}
	password, _ := proxyURL.User.Password()
	headers.Set("Proxy-Authorization", "Basic "+basicAuth(proxyURL.User.Username(), password))
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

type closeOnClose struct {
	io.ReadCloser
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func (body *closeOnClose) Close() error {
	var err error
	body.once.Do(func() {
		close(body.done)
		if readErr := body.ReadCloser.Close(); readErr != nil {
			err = readErr
		}
		if connErr := body.conn.Close(); err == nil && connErr != nil {
			err = connErr
		}
	})
	return err
}

func tlsConfigForServerName(base *tls.Config, serverName string) *tls.Config {
	if base == nil {
		return &tls.Config{ServerName: serverName}
	}
	config := base.Clone()
	config.ServerName = serverName
	return config
}

func pinRequest(req *http.Request, resolver resolver) (*http.Request, string, error) {
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	var ip net.IP
	if literal := net.ParseIP(host); literal != nil {
		if isBlockedIP(literal) {
			return nil, "", errors.New("blocked address")
		}
		ip = literal
	} else {
		ips, err := resolver.LookupIPAddr(req.Context(), host)
		if err != nil {
			return nil, "", fmt.Errorf("dns: %w", err)
		}
		if len(ips) == 0 {
			return nil, "", errors.New("dns: no addresses")
		}
		for _, candidate := range ips {
			if isBlockedIP(candidate.IP) {
				return nil, "", errors.New("blocked address")
			}
		}
		ip = ips[0].IP
	}
	pinned := req.Clone(req.Context())
	pinned.URL.Host = net.JoinHostPort(ip.String(), port)
	if pinned.Host == "" {
		pinned.Host = req.URL.Host
	}
	return pinned, host, nil
}

func boundedTimeout(total, maximum time.Duration) time.Duration {
	if total > 0 && total < maximum {
		return total
	}
	return maximum
}

func safeDialContext(resolver resolver, dialer dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid dial address: %w", err)
		}
		if err := validatePort(port); err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if isBlockedIP(ip) {
				return nil, errors.New("blocked address")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("dns: %w", err)
		}
		if len(ips) == 0 {
			return nil, errors.New("dns: no addresses")
		}
		for _, ip := range ips {
			if isBlockedIP(ip.IP) {
				return nil, errors.New("blocked address")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}
