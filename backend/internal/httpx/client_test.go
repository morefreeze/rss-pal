package httpx

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
}

func (r fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.lookup(ctx, host)
}

type recordingDialer struct {
	mu    sync.Mutex
	calls []string
	dial  func(context.Context, string, string) (net.Conn, error)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.calls = append(d.calls, address)
	d.mu.Unlock()
	return d.dial(ctx, network, address)
}

func (d *recordingDialer) addresses() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string // substring; "" means expect success
	}{
		{"http public host", "http://8.8.8.8/x.jpg", ""},
		{"https public host", "https://8.8.8.8/x.jpg", ""},
		{"empty", "", "empty url"},
		{"ftp scheme", "ftp://example.com/x", "unsupported scheme"},
		{"no host", "http:///x", "missing host"},
		{"empty hostname", "http://:8080/x", "missing host"},
		{"loopback ipv4", "http://127.0.0.1/x", "blocked address"},
		{"rfc1918", "http://10.0.0.5/x", "blocked address"},
		{"link-local", "http://169.254.169.254/latest/meta-data/", "blocked address"},
		{"loopback ipv6", "http://[::1]/x", "blocked address"},
		{"ipv4 unspecified", "http://0.0.0.0/x", "blocked address"},
		{"ipv4 multicast", "http://224.0.0.1/x", "blocked address"},
		{"ipv6 ula", "http://[fc00::1]/x", "blocked address"},
		{"ipv6 multicast", "http://[ff02::1]/x", "blocked address"},
		{"unsafe service port", "https://8.8.8.8:2375/x", "blocked port"},
		{"invalid port", "https://8.8.8.8:bad/x", "invalid port"},
		{"allowed web port", "https://8.8.8.8:8443/x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateURLRejectsAnyBlockedDNSResult(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.7")}}, nil
	}}

	_, err := validateURL(context.Background(), "https://safe.example/feed", resolver)
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("expected blocked DNS response to fail closed, got %v", err)
	}
}

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.20.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			got := isBlockedIP(net.ParseIP(tc.ip))
			if got != tc.want {
				t.Fatalf("ip=%s want=%v got=%v", tc.ip, tc.want, got)
			}
		})
	}
}

func TestValidateURLRejectsCredentials(t *testing.T) {
	_, err := ValidateURL("https://user:pass@8.8.8.8/feed")
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentials to be rejected, got %v", err)
	}
}

func TestClientRejectsDNSRebindingBeforeDial(t *testing.T) {
	lookups := 0
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups <= 2 {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.7")}}, nil
	}}
	if _, err := validateURL(context.Background(), "http://safe.example/feed", resolver); err != nil {
		t.Fatalf("preflight validation failed: %v", err)
	}
	dialer := &recordingDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial must not run")
	}}

	_, err := newClient(time.Second, resolver, dialer).Get("http://safe.example/feed")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("expected rebinding to be rejected, got %v", err)
	}
	if calls := dialer.addresses(); len(calls) != 0 {
		t.Fatalf("blocked address reached dialer: %v", calls)
	}
}

func TestClientDialsValidatedResolvedIP(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}}
	dialer := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = http.ReadRequest(bufio.NewReader(server))
			_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}()
		return client, nil
	}}

	response, err := newClient(time.Second, resolver, dialer).Get("http://safe.example:8080/feed")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	if body, _ := io.ReadAll(response.Body); string(body) != "ok" {
		t.Fatalf("unexpected body %q", body)
	}
	if got := dialer.addresses(); len(got) != 1 || got[0] != "93.184.216.34:8080" {
		t.Fatalf("want dial of resolved IP, got %v", got)
	}
}

func TestClientFailsClosedWhenDNSContainsBlockedIP(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("192.168.1.5")}}, nil
	}}
	dialer := &recordingDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial must not run")
	}}

	_, err := newClient(time.Second, resolver, dialer).Get("http://safe.example/feed")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("expected mixed DNS response to fail closed, got %v", err)
	}
	if calls := dialer.addresses(); len(calls) != 0 {
		t.Fatalf("blocked address reached dialer: %v", calls)
	}
}

func TestClientRejectsUnsafePortBeforeDial(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}}
	dialer := &recordingDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial must not run")
	}}

	_, err := newClient(time.Second, resolver, dialer).Get("http://safe.example:2375/feed")
	if err == nil || !strings.Contains(err.Error(), "blocked port") {
		t.Fatalf("expected unsafe port to be rejected, got %v", err)
	}
	if calls := dialer.addresses(); len(calls) != 0 {
		t.Fatalf("unsafe port reached dialer: %v", calls)
	}
}

func TestClientProxyUsesPinnedTargetAndOriginalHost(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}}
	proxyURL, err := url.Parse("http://user:pass@127.0.0.1:8888")
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 1)
	proxyDialer := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			var raw strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				raw.WriteString(line)
				if line == "\r\n" {
					requests <- raw.String()
					break
				}
			}
			_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}()
		return client, nil
	}}
	client := newClientWithProxy(time.Second, resolver, proxyDialer, func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	})

	response, err := client.Get("http://safe.example/feed")
	if err != nil {
		t.Fatalf("proxied request failed: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	proxyRequest := <-requests
	if !strings.Contains(proxyRequest, "GET http://93.184.216.34:80/feed HTTP/1.1\r\n") || !strings.Contains(proxyRequest, "Host: safe.example\r\n") || !strings.Contains(proxyRequest, "Proxy-Authorization: Basic dXNlcjpwYXNz\r\n") {
		t.Fatalf("proxy request did not preserve pinned target and original host: %q", proxyRequest)
	}
	if got := proxyDialer.addresses(); len(got) != 1 || got[0] != "127.0.0.1:8888" {
		t.Fatalf("want proxy dial, got %v", got)
	}
}

func TestClientHTTPSProxyPinsConnectAndPreservesSNIAndHost(t *testing.T) {
	certificate, roots := testCertificate(t)
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}}
	proxyURL, err := url.Parse("https://user:pass@proxy.example:8888")
	if err != nil {
		t.Fatal(err)
	}
	type capture struct{ connect, proxySNI, sni, host, auth string }
	captures := make(chan capture, 1)
	proxyDialer := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var proxySNI string
			proxyTLS := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{certificate}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				proxySNI = hello.ServerName
				return nil, nil
			}})
			if err := proxyTLS.Handshake(); err != nil {
				return
			}
			connect, err := http.ReadRequest(bufio.NewReader(proxyTLS))
			if err != nil {
				return
			}
			_, _ = io.WriteString(proxyTLS, "HTTP/1.1 200 Connection Established\r\n\r\n")
			var sni string
			tlsServer := tls.Server(proxyTLS, &tls.Config{Certificates: []tls.Certificate{certificate}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				sni = hello.ServerName
				return nil, nil
			}})
			if err := tlsServer.Handshake(); err != nil {
				return
			}
			origin, err := http.ReadRequest(bufio.NewReader(tlsServer))
			if err != nil {
				return
			}
			captures <- capture{connect: connect.Host, proxySNI: proxySNI, sni: sni, host: origin.Host, auth: connect.Header.Get("Proxy-Authorization")}
			_, _ = io.WriteString(tlsServer, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}()
		return client, nil
	}}
	client := newClientWithProxyAndTLS(time.Second, resolver, proxyDialer, func(*http.Request) (*url.URL, error) { return proxyURL, nil }, &tls.Config{RootCAs: roots})

	response, err := client.Get("https://safe.example/feed")
	if err != nil {
		t.Fatalf("proxied HTTPS request failed: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	got := <-captures
	if got.connect != "93.184.216.34:443" || got.proxySNI != "proxy.example" || got.sni != "safe.example" || got.host != "safe.example" {
		t.Fatalf("CONNECT=%q proxySNI=%q SNI=%q Host=%q", got.connect, got.proxySNI, got.sni, got.host)
	}
	if got.auth == "" {
		t.Fatal("missing proxy authorization")
	}
}

func TestClientHTTPSProxyRejectsConnectFailureAndHonorsCancellation(t *testing.T) {
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}}
	proxyURL, _ := url.Parse("http://127.0.0.1:8888")
	failedProxy := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = http.ReadRequest(bufio.NewReader(server))
			_, _ = io.WriteString(server, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		}()
		return client, nil
	}}
	client := newClientWithProxy(time.Second, resolver, failedProxy, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
	_, err := client.Get("https://safe.example/feed")
	if err == nil || !strings.Contains(err.Error(), "proxy CONNECT returned HTTP status 502") {
		t.Fatalf("expected CONNECT failure, got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	connected := make(chan struct{})
	cancelledDialer := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = http.ReadRequest(bufio.NewReader(server))
			close(connected)
			_, _ = io.Copy(io.Discard, server)
		}()
		return client, nil
	}}
	client = newClientWithProxy(time.Second, resolver, cancelledDialer, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://safe.example/feed", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	<-connected
}

func TestProxyAddressUsesSchemeDefaultPort(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{raw: "http://proxy.example", want: "proxy.example:80"},
		{raw: "https://proxy.example", want: "proxy.example:443"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			proxyURL, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := proxyAddress(proxyURL); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "safe.example"},
		DNSNames:     []string{"safe.example", "proxy.example"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certificatePEM)
	return certificate, roots
}

func TestClientRejectsRedirectToPrivateAddress(t *testing.T) {
	client := newClient(time.Second, fakeResolver{}, &recordingDialer{})
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://10.0.0.7/private"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	_, err := client.Get("http://8.8.8.8/start")
	if err == nil || !strings.Contains(err.Error(), "redirect rejected") {
		t.Fatalf("expected private redirect to be rejected, got %v", err)
	}
}

func TestClientStopsAfterTooManyRedirects(t *testing.T) {
	client := newClient(time.Second, fakeResolver{}, &recordingDialer{})
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://8.8.8.8/again"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	_, err := client.Get("http://8.8.8.8/start")
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}

func TestFetchBoundedReturnsExactLimitAndRequestHeaders(t *testing.T) {
	var received http.Header
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		received = req.Header.Clone()
		header := make(http.Header)
		header.Set("ETag", "v1")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("four")), Request: req}, nil
	})}

	result, err := FetchBounded(context.Background(), client, "https://8.8.8.8/feed", http.Header{"X-Trace": []string{"trace-1"}}, 4)
	if err != nil {
		t.Fatalf("FetchBounded failed: %v", err)
	}
	if result.StatusCode != http.StatusOK || string(result.Body) != "four" || result.Header.Get("ETag") != "v1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if received.Get("X-Trace") != "trace-1" || received.Get("User-Agent") != UserAgent {
		t.Fatalf("missing request headers: %v", received)
	}
}

func TestFetchBoundedReportsFinalResponseURL(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		finalRequest := req.Clone(req.Context())
		finalRequest.URL, _ = url.Parse("https://8.8.8.8/final/feed.xml")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: finalRequest}, nil
	})}

	result, err := FetchBounded(context.Background(), client, "https://8.8.8.8/start", nil, 2)
	if err != nil {
		t.Fatalf("FetchBounded failed: %v", err)
	}
	if result.FinalURL != "https://8.8.8.8/final/feed.xml" {
		t.Fatalf("FinalURL = %q", result.FinalURL)
	}
}

func TestFetchBoundedRejectsUnsafeRedirectWithOrdinaryClient(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://127.0.0.1/private"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	})}

	_, err := FetchBounded(context.Background(), client, "https://8.8.8.8/start", nil, 2)
	if err == nil || !strings.Contains(err.Error(), "redirect rejected") {
		t.Fatalf("unsafe redirect err=%v", err)
	}
	if requests != 1 {
		t.Fatalf("unsafe redirect reached transport: requests=%d", requests)
	}
}

func TestFetchBoundedRejectsResponseOverLimit(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fives")), Request: req}, nil
	})}

	_, err := FetchBounded(context.Background(), client, "https://8.8.8.8/feed", nil, 4)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}
}

func TestFetchBoundedReturnsNon2xxStatusAndError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"X-Reason": []string{"gone"}}, Body: io.NopCloser(strings.NewReader("missing")), Request: req}, nil
	})}

	result, err := FetchBounded(context.Background(), client, "https://8.8.8.8/feed", nil, 100)
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status 404") {
		t.Fatalf("expected status error, got %v", err)
	}
	if result == nil || result.StatusCode != http.StatusNotFound || string(result.Body) != "missing" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFetchBoundedRejectsNilClientAndCancelledContext(t *testing.T) {
	if _, err := FetchBounded(context.Background(), nil, "https://8.8.8.8/feed", nil, 1); err == nil || !strings.Contains(err.Error(), "client") {
		t.Fatalf("expected nil client error, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}
	_, err := FetchBounded(ctx, client, "https://8.8.8.8/feed", nil, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
