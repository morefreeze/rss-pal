package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBufferedGETRecoversFromUnusableFirstAddress(t *testing.T) {
	for _, first := range []string{"hang", "partial 200", "403", "500"} {
		t.Run(first, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				ready := make(chan struct{})
				closed := make(chan struct{})
				client, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(address string, _ *http.Request, conn net.Conn) {
					if strings.HasPrefix(address, "93.184.216.34:") {
						switch first {
						case "partial 200":
							_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
						case "403", "500":
							_, _ = fmt.Fprintf(conn, "HTTP/1.1 %s Error\r\nContent-Length: 0\r\n\r\n", first)
							close(ready)
							close(closed)
							return
						}
						close(ready)
						_, _ = io.Copy(io.Discard, conn)
						close(closed)
						return
					}
					<-ready
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 16\r\n\r\ncomplete article")
				})
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/article", nil)
				response, err := client.Do(req)
				if err != nil {
					t.Fatalf("complete candidate unavailable: %v; dials=%v", err, dialer.addresses())
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || string(body) != "complete article" || response.StatusCode != http.StatusOK {
					t.Fatalf("winner status=%d body=%q error=%v", response.StatusCode, body, err)
				}
				if ctx.Err() != nil {
					t.Fatalf("candidate race exhausted the caller: %v", ctx.Err())
				}
				<-closed
				if got := response.Request.URL.String(); got != "http://safe.example/article" {
					t.Fatalf("response identity = %q", got)
				}
			})
		})
	}
}

func TestBufferedGETFirstCompleteAddressCancelsPartialLoser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		partial := make(chan struct{})
		closed := make(chan struct{})
		client, _ := bufferedGETFixture(t, publicGETAddresses(2), func(address string, _ *http.Request, conn net.Conn) {
			if strings.HasPrefix(address, "8.8.8.8:") {
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				close(partial)
				_, _ = io.Copy(io.Discard, conn)
				close(closed)
				return
			}
			<-partial
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		})
		response, err := client.Get("http://safe.example/article")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		<-closed
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != "ok" {
			t.Fatalf("complete response = %q, %v", body, err)
		}
	})
}

func TestBufferedGETBoundsAndCompletesBodies(t *testing.T) {
	for _, scenario := range []string{"exact limit", "declared oversized", "chunked oversized", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			client, _ := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
				switch scenario {
				case "declared oversized":
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", bufferedGETBodyLimit+1)
				case "chunked oversized":
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n", bufferedGETBodyLimit+1)
					_, _ = io.WriteString(conn, strings.Repeat("x", bufferedGETBodyLimit+1)+"\r\n0\r\n\r\n")
				case "truncated":
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				default:
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", bufferedGETBodyLimit)
					_, _ = io.WriteString(conn, strings.Repeat("x", bufferedGETBodyLimit))
				}
			})
			response, err := client.Get("http://safe.example/article")
			if scenario != "exact limit" {
				if err == nil || response != nil {
					t.Fatalf("incomplete or oversized response accepted: %v, %v", response, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || len(body) != bufferedGETBodyLimit {
				t.Fatalf("exact limit body: bytes=%d error=%v", len(body), err)
			}
		})
	}
}

func TestBufferedGETClosesOversizedProxyResponseWithoutDraining(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopServer := make(chan struct{})
		client, _ := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
			_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", bufferedGETBodyLimit+1)
			<-stopServer
		})
		proxyURL, _ := url.Parse("http://proxy.example:8888")
		client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/article", nil)
			_, err := client.Do(req)
			finished <- err
		}()
		select {
		case err := <-finished:
			close(stopServer)
			if err == nil || !strings.Contains(err.Error(), "exceeds") || ctx.Err() != nil {
				t.Errorf("oversized response was not rejected promptly: %v; context=%v", err, ctx.Err())
			}
		case <-ctx.Done():
			close(stopServer)
			<-finished
			t.Error("proxy body cleanup waited for data past the caller deadline")
		}
	})
}

func TestBufferedGETRejectsEntireUnsafeDNSAnswerBeforeDial(t *testing.T) {
	for _, unsafe := range []string{"10.0.0.7", "198.18.0.7", "::1"} {
		t.Run(unsafe, func(t *testing.T) {
			addresses := append(publicGETAddresses(4), net.IPAddr{IP: net.ParseIP(unsafe)})
			client, dialer := bufferedGETFixture(t, addresses, func(string, *http.Request, net.Conn) {
				t.Error("unsafe DNS answer reached network")
			})
			_, err := client.Get("http://safe.example/article")
			if err == nil || len(dialer.addresses()) != 0 {
				t.Fatalf("unsafe answer: error=%v dials=%v", err, dialer.addresses())
			}
		})
	}
}

func TestBufferedGETCapsUniqueCandidatesAndReturnsAllFailures(t *testing.T) {
	addresses := publicGETAddresses(6)
	addresses = append(addresses[:1], addresses...)
	client, dialer := bufferedGETFixture(t, addresses, func(string, *http.Request, net.Conn) {})
	if _, err := client.Get("http://safe.example/article"); err == nil {
		t.Fatal("all failed candidates returned success")
	}
	seen := make(map[string]bool)
	for _, address := range dialer.addresses() {
		seen[address] = true
	}
	if len(dialer.addresses()) != 4 || len(seen) != 4 {
		t.Fatalf("candidates = %v, want four unique addresses", dialer.addresses())
	}
}

func TestBufferedGETRetainsHTTPErrorWhenNoAddressSucceeds(t *testing.T) {
	client, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, _ *http.Request, conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 6\r\n\r\ndenied")
	})
	response, err := client.Get("http://safe.example/article")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusForbidden || string(body) != "denied" || len(dialer.addresses()) != 2 {
		t.Fatalf("fallback HTTP response: status=%d body=%q error=%v dials=%v", response.StatusCode, body, err, dialer.addresses())
	}
}

func TestBufferedGETCancellationClosesConnectionsAndPendingPermits(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var closed atomic.Int32
				client, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, _ *http.Request, conn net.Conn) {
					_, _ = io.Copy(io.Discard, conn)
					closed.Add(1)
				})
				transport := client.Transport.(*bufferedGETTransport)
				transport.slots = make(chan struct{}, 4)
				if pending {
					for range 4 {
						transport.slots <- struct{}{}
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/article", nil)
				if _, err := client.Do(req); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("cancellation error = %v", err)
				}
				synctest.Wait()
				want := 4
				if pending {
					want = 0
				}
				if len(dialer.addresses()) != want || int(closed.Load()) != want {
					t.Fatalf("after cancellation: dials=%v closed=%d want=%d", dialer.addresses(), closed.Load(), want)
				}
			})
		})
	}
}

func TestBufferedGETSharesCandidatePermitsAcrossClients(t *testing.T) {
	first := NewBufferedGETClient(time.Second).Transport.(*bufferedGETTransport)
	second := NewBufferedGETClient(time.Second).Transport.(*bufferedGETTransport)
	if first.slots != second.slots || cap(first.slots) != 4 {
		t.Fatal("manual clients do not share the four-connection bound")
	}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		slots := make(chan struct{}, 4)
		results := make(chan error, 2)
		var dialers []*recordingDialer
		var closed atomic.Int32
		for range 2 {
			client, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, _ *http.Request, conn net.Conn) {
				_, _ = io.Copy(io.Discard, conn)
				closed.Add(1)
			})
			client.Transport.(*bufferedGETTransport).slots = slots
			dialers = append(dialers, dialer)
			go func() {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/article", nil)
				_, err := client.Do(req)
				results <- err
			}()
		}
		synctest.Wait()
		if total := len(dialers[0].addresses()) + len(dialers[1].addresses()); total != 4 {
			t.Errorf("concurrent clients opened %d candidates, want shared limit 4", total)
		}
		cancel()
		for range 2 {
			if err := <-results; !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled concurrent request = %v", err)
			}
		}
		synctest.Wait()
		if closed.Load() != 4 || len(slots) != 0 {
			t.Errorf("candidate resources after cancellation: closed=%d permits=%d", closed.Load(), len(slots))
		}
	})
}

func TestBufferedGETPreservesLegacyAndNonPublicStreaming(t *testing.T) {
	for _, scenario := range []string{"legacy GET", "POST", "authenticated GET", "GET body"} {
		t.Run(scenario, func(t *testing.T) {
			client, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, request *http.Request, conn net.Conn) {
				if request.Body != nil {
					_, _ = io.Copy(io.Discard, request.Body)
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				_, _ = io.Copy(io.Discard, conn)
			})
			if scenario == "legacy GET" {
				client.Transport = client.Transport.(*bufferedGETTransport).base
			}
			method := http.MethodGet
			var body io.Reader
			if scenario == "POST" || scenario == "GET body" {
				body = strings.NewReader("request body")
			}
			if scenario == "POST" {
				method = http.MethodPost
			}
			req, _ := http.NewRequest(method, "http://safe.example/article", body)
			if scenario == "authenticated GET" {
				req.Header.Set("Authorization", "test-authentication")
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			prefix := make([]byte, 6)
			_, err = io.ReadFull(response.Body, prefix)
			_ = response.Body.Close()
			if err != nil || string(prefix) != "prefix" || len(dialer.addresses()) != 1 {
				t.Fatalf("streaming behavior: prefix=%q error=%v dials=%v", prefix, err, dialer.addresses())
			}
		})
	}
}

func TestBufferedGETProxyPreservesConnectSNIHostAndCompleteBody(t *testing.T) {
	certificate, roots := testCertificate(t)
	addresses := publicGETAddresses(2)
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil }}
	proxyURL, _ := url.Parse("https://proxy.example:8888")
	type capture struct{ target, proxySNI, originSNI, host string }
	captures := make(chan capture, 2)
	partial := make(chan struct{})
	closed := make(chan struct{})
	dialer := &recordingDialer{dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var proxySNI, originSNI string
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
			originTLS := tls.Server(proxyTLS, &tls.Config{Certificates: []tls.Certificate{certificate}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				originSNI = hello.ServerName
				return nil, nil
			}})
			if err := originTLS.Handshake(); err != nil {
				return
			}
			request, err := http.ReadRequest(bufio.NewReader(originTLS))
			if err != nil {
				return
			}
			captures <- capture{connect.Host, proxySNI, originSNI, request.Host}
			if connect.Host == "93.184.216.34:443" {
				_, _ = io.WriteString(originTLS, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				close(partial)
				_, _ = io.Copy(io.Discard, originTLS)
				close(closed)
				return
			}
			<-partial
			_, _ = io.WriteString(originTLS, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}()
		return client, nil
	}}
	client := withBufferedGETAddressFallback(newClientWithProxyAndTLS(2*time.Second, resolver, dialer, func(*http.Request) (*url.URL, error) { return proxyURL, nil }, &tls.Config{RootCAs: roots}))
	response, err := client.Get("https://safe.example/article")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	for range 2 {
		got := <-captures
		if (got.target != "93.184.216.34:443" && got.target != "8.8.8.8:443") || got.proxySNI != "proxy.example" || got.originSNI != "safe.example" || got.host != "safe.example" {
			t.Fatalf("proxy identity = %#v", got)
		}
	}
	<-closed
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "ok" || response.Request.URL.String() != "https://safe.example/article" {
		t.Fatalf("complete proxied response: body=%q error=%v URL=%s", body, err, response.Request.URL)
	}
	for _, address := range dialer.addresses() {
		if address != "proxy.example:8888" {
			t.Fatalf("request bypassed configured proxy: %s", address)
		}
	}
}

func TestBufferedGETRedirectPreservesPublicIdentityAndValidation(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, viaProxy := range []bool{false, true} {
			t.Run(fmt.Sprintf("private=%t/proxy=%t", private, viaProxy), func(t *testing.T) {
				client, _ := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, request *http.Request, conn net.Conn) {
					// ReadRequest represents an absolute-form proxy target in Host;
					// the wire Host header is covered by the proxy identity tests.
					if !viaProxy && request.Host != "safe.example" {
						t.Errorf("redirect Host = %q", request.Host)
					}
					if request.URL.Path == "/start" {
						location := "/article"
						if private {
							location = "http://127.0.0.1/private"
						}
						_, _ = fmt.Fprintf(conn, "HTTP/1.1 302 Found\r\nLocation: %s\r\nContent-Length: 0\r\n\r\n", location)
						return
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
				})
				if viaProxy {
					proxyURL, _ := url.Parse("http://proxy.example:8888")
					client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
				}
				response, err := client.Get("http://safe.example/start")
				if private {
					if err == nil || !strings.Contains(err.Error(), "blocked address") {
						t.Fatalf("unsafe redirect accepted: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.Request.URL.String() != "http://safe.example/article" {
					t.Fatalf("relative redirect identity = %s", response.Request.URL)
				}
			})
		}
	}
}

func publicGETAddresses(count int) []net.IPAddr {
	addresses := []string{"93.184.216.34", "8.8.8.8", "1.1.1.1", "9.9.9.9", "8.8.4.4", "1.0.0.1"}
	result := make([]net.IPAddr, 0, count)
	for _, address := range addresses[:count] {
		result = append(result, net.IPAddr{IP: net.ParseIP(address)})
	}
	return result
}

func bufferedGETFixture(t *testing.T, addresses []net.IPAddr, serve func(string, *http.Request, net.Conn)) (*http.Client, *recordingDialer) {
	t.Helper()
	resolver := fakeResolver{lookup: func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil }}
	dialer := &recordingDialer{dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			request, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			serve(address, request, server)
		}()
		return client, nil
	}}
	client := newClient(30*time.Second, resolver, dialer)
	return withBufferedGETAddressFallback(client), dialer
}
