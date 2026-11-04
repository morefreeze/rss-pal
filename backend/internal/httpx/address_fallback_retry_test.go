package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// Reproduce the observed image failure without contacting a public endpoint:
// both IPv4 peers close before an image response and both IPv6 CONNECTs fail.
// A fresh connection to the same checked IPv4 peer then serves the full image.
func TestBufferedImageRecoversOnceFromPreBodyEOF(t *testing.T) {
	for _, phase := range []string{"CONNECT headers", "TLS handshake", "origin headers"} {
		t.Run(phase, func(t *testing.T) {
			certificate, roots := testCertificate(t)
			addresses := append(publicGETAddresses(2),
				net.IPAddr{IP: net.ParseIP("2606:4700:3035::ac43:adad")},
				net.IPAddr{IP: net.ParseIP("2606:4700:3037::6815:5024")})
			var mu sync.Mutex
			attempts := make(map[string]int)
			fixture, dialer := bufferedGETFixture(t, addresses, func(_ string, connect *http.Request, conn net.Conn) {
				if connect.Method != http.MethodConnect {
					t.Error("expected a pinned CONNECT")
					return
				}
				mu.Lock()
				attempts[connect.Host]++
				attempt := attempts[connect.Host]
				mu.Unlock()
				if strings.HasPrefix(connect.Host, "[") {
					_, _ = io.WriteString(conn, "HTTP/1.1 500 Proxy Failure\r\nContent-Length: 0\r\n\r\n")
					return
				}
				if attempt == 1 && phase == "CONNECT headers" {
					return
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				if attempt == 1 && phase == "TLS handshake" {
					// Consume the ClientHello so the simulated peer closes while
					// the client reads, as a TCP peer would, rather than while
					// net.Pipe's synchronous Write is still pending.
					header := make([]byte, 5)
					if _, err := io.ReadFull(conn, header); err == nil {
						_, _ = io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint16(header[3:5])))
					}
					return
				}
				origin := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
				if err := origin.Handshake(); err != nil {
					return // A winning peer may cancel this connection.
				}
				request, err := http.ReadRequest(bufio.NewReader(origin))
				if err != nil {
					return
				}
				if request.Host != "safe.example" || origin.ConnectionState().ServerName != "safe.example" ||
					request.Header.Get("Referer") != "https://safe.example/" || request.Header.Get("User-Agent") != UserAgent {
					t.Error("fresh connection changed public Host/SNI or image headers")
				}
				if attempt == 1 && phase == "origin headers" {
					return
				}
				_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: 14\r\n\r\ncomplete image")
			})
			client := imageRetryFixture(fixture)
			transport := client.Transport.(*bufferedGETTransport)
			proxyURL, _ := url.Parse("http://proxy.example:8888")
			transport.base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
			transport.base.tlsConfig = &tls.Config{RootCAs: roots}
			req, _ := http.NewRequest(http.MethodGet, "https://safe.example/image.png", nil)
			req.Header.Set("Referer", "https://safe.example/")
			req.Header.Set("User-Agent", UserAgent)
			response, err := client.Do(req)
			if err != nil {
				t.Fatalf("transient pre-body failure exhausted valid peers before the deadline: %v", err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(body) != "complete image" || response.StatusCode != http.StatusOK || response.Request.URL.String() != req.URL.String() {
				t.Fatalf("fresh image response: status=%d body=%q err=%v", response.StatusCode, body, err)
			}
			mu.Lock()
			defer mu.Unlock()
			for target, count := range attempts {
				limit := 2
				if strings.HasPrefix(target, "[") {
					limit = 1 // A known failed CONNECT status is not transient EOF.
				}
				if count > limit {
					t.Errorf("target %s attempted %d times, limit %d", target, count, limit)
				}
			}
			if count := len(dialer.addresses()); count < 5 || count > 6 || len(transport.slots) != 0 {
				t.Fatalf("fresh wave connections=%d permits=%d", count, len(transport.slots))
			}
		})
	}
}

func TestBufferedImageRecoveryIsBoundedAndKeepsPublicGETScope(t *testing.T) {
	for _, scenario := range []string{"persistent EOF", "CONNECT 500", "HTTP 500", "partial image", "oversize image", "authorization", "cookie", "POST", "GET body", "HTML client"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, dialer := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, req *http.Request, conn net.Conn) {
				switch scenario {
				case "CONNECT 500":
					_, _ = io.WriteString(conn, "HTTP/1.1 500 Proxy Failure\r\nContent-Length: 0\r\n\r\n")
				case "HTTP 500":
					_, _ = io.WriteString(conn, "HTTP/1.1 500 Origin Failure\r\nContent-Length: 0\r\n\r\n")
				case "partial image":
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				case "oversize image":
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", bufferedImageBodyLimit+1)
				}
			})
			client := imageRetryFixture(fixture)
			if scenario == "HTML client" {
				client = fixture
			}
			if scenario == "CONNECT 500" {
				proxyURL, _ := url.Parse("http://proxy.example:8888")
				client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
			}
			method, target := http.MethodGet, "http://safe.example/image.png"
			if scenario == "CONNECT 500" {
				target = "https://safe.example/image.png"
			}
			if scenario == "POST" {
				method = http.MethodPost
			}
			var body io.Reader
			if scenario == "GET body" {
				body = strings.NewReader("request entity")
			}
			req, _ := http.NewRequest(method, target, body)
			if scenario == "authorization" {
				req.Header.Set("Authorization", "test-only")
			}
			if scenario == "cookie" {
				req.Header.Set("Cookie", "test-only")
			}
			response, _ := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
			}
			want := 1
			if scenario == "persistent EOF" {
				want = 2
			}
			if got := len(dialer.addresses()); got != want {
				t.Fatalf("%s made %d attempts, want %d", scenario, got, want)
			}
			if permits := len(client.Transport.(*bufferedGETTransport).slots); permits != 0 {
				t.Fatalf("request retained %d permits", permits)
			}
		})
	}
}

func TestBufferedImageRecoveryKeepsOriginalDeadlineAndCancellation(t *testing.T) {
	for _, caller := range []string{"deadline", "cancel"} {
		t.Run(caller, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var count, closed atomic.Int32
				retrying := make(chan struct{})
				fixture, dialer := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
					defer closed.Add(1)
					if count.Add(1) == 1 {
						time.Sleep(60 * time.Second)
						return
					}
					close(retrying)
					_, _ = io.Copy(io.Discard, conn)
				})
				client := imageRetryFixture(fixture)
				proxyURL, _ := url.Parse("http://proxy.example:8888")
				client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/image.png", nil)
				start := time.Now()
				done := make(chan error, 1)
				go func() {
					_, err := client.Do(req)
					done <- err
				}()
				select {
				case <-retrying:
				case err := <-done:
					t.Fatalf("recovery did not start after transient EOF: %v", err)
				}
				if caller == "cancel" {
					cancel()
				}
				err := <-done
				wantError, wantElapsed := context.DeadlineExceeded, 90*time.Second
				if caller == "cancel" {
					wantError, wantElapsed = context.Canceled, 60*time.Second
				}
				synctest.Wait()
				if !errors.Is(err, wantError) || time.Since(start) != wantElapsed || len(dialer.addresses()) != 2 || closed.Load() != 2 || len(client.Transport.(*bufferedGETTransport).slots) != 0 {
					t.Fatalf("recovery changed caller budget or leaked: err=%v elapsed=%v dials=%v closed=%d permits=%d", err, time.Since(start), dialer.addresses(), closed.Load(), len(client.Transport.(*bufferedGETTransport).slots))
				}
			})
		})
	}
}

func TestBufferedImageRecoversOnceFromConnectionReset(t *testing.T) {
	fixture, dialer := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	})
	var count atomic.Int32
	dial := dialer.dial
	dialer.dial = func(ctx context.Context, network, target string) (net.Conn, error) {
		if count.Add(1) == 1 {
			return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
		}
		return dial(ctx, network, target)
	}
	response, err := imageRetryFixture(fixture).Get("http://safe.example/image.png")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if len(dialer.addresses()) != 2 {
		t.Fatalf("reset recovery attempts=%v", dialer.addresses())
	}
}

func TestBufferedImageWinnerDoesNotStartFreshConnections(t *testing.T) {
	failed := make(chan struct{})
	fixture, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(target string, _ *http.Request, conn net.Conn) {
		if strings.HasPrefix(target, "93.184.216.34:") {
			close(failed)
			return
		}
		<-failed
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	})
	client := imageRetryFixture(fixture)
	response, err := client.Get("http://safe.example/image.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(dialer.addresses()) != 2 || len(client.Transport.(*bufferedGETTransport).slots) != 0 {
		t.Fatalf("complete winner started recovery or retained permits: dials=%v", dialer.addresses())
	}
}

func TestBufferedImageWinnerCancelsRealProxyAtEveryPendingPhase(t *testing.T) {
	for _, phase := range []string{"CONNECT headers", "TLS handshake", "origin headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			certificate, roots := testCertificate(t)
			certificateTime := time.Now()
			synctest.Test(t, func(t *testing.T) {
				pending, closed := make(chan struct{}), make(chan struct{})
				fixture, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, connect *http.Request, conn net.Conn) {
					loser := connect.Host == "8.8.8.8:443"
					if loser {
						defer close(closed)
					}
					if loser && phase == "CONNECT headers" {
						close(pending)
						_, _ = io.Copy(io.Discard, conn)
						return
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					if loser && phase == "TLS handshake" {
						header := make([]byte, 5)
						if _, err := io.ReadFull(conn, header); err == nil {
							_, _ = io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint16(header[3:5])))
						}
						close(pending)
						_, _ = io.Copy(io.Discard, conn)
						return
					}
					origin := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
					if err := origin.Handshake(); err != nil {
						return
					}
					if _, err := http.ReadRequest(bufio.NewReader(origin)); err != nil {
						return
					}
					if loser {
						if phase == "body" {
							_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
						}
						close(pending)
						_, _ = io.Copy(io.Discard, origin)
						return
					}
					<-pending
					_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
				})
				client := imageRetryFixture(fixture)
				transport := client.Transport.(*bufferedGETTransport)
				proxyURL, _ := url.Parse("http://proxy.example:8888")
				transport.base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
				transport.base.tlsConfig = &tls.Config{RootCAs: roots, Time: func() time.Time { return certificateTime }}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://safe.example/image.png", nil)
				response, err := client.Do(req)
				if err != nil {
					t.Fatalf("pending %s held the complete winner: %v", phase, err)
				}
				_ = response.Body.Close()
				select {
				case <-closed:
				case <-ctx.Done():
					t.Fatal("loser socket was not closed before the original deadline")
				}
				if ctx.Err() != nil || len(dialer.addresses()) != 2 || len(transport.slots) != 0 {
					t.Fatalf("winner waited for loser deadline or started recovery: err=%v dials=%v permits=%d", ctx.Err(), dialer.addresses(), len(transport.slots))
				}
			})
		})
	}
}

func TestBufferedImageRecoveryDoesNotExposePartialOversizeOrBlockedRedirect(t *testing.T) {
	for _, scenario := range []string{"partial", "oversize", "blocked redirect"} {
		t.Run(scenario, func(t *testing.T) {
			var count atomic.Int32
			fixture, dialer := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
				if count.Add(1) == 1 {
					return
				}
				switch scenario {
				case "partial":
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
				case "oversize":
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", bufferedImageBodyLimit+1)
				case "blocked redirect":
					_, _ = io.WriteString(conn, "HTTP/1.1 302 Found\r\nLocation: http://10.0.0.7/private\r\nContent-Length: 0\r\n\r\n")
				}
			})
			response, err := imageRetryFixture(fixture).Get("http://safe.example/image.png")
			if response != nil || err == nil || len(dialer.addresses()) != 2 {
				t.Fatalf("unsafe recovery response=%v err=%v dials=%v", response, err, dialer.addresses())
			}
			if scenario == "blocked redirect" && !strings.Contains(err.Error(), "blocked address") {
				t.Fatalf("unsafe redirect was not validated: %v", err)
			}
		})
	}
}

func TestBufferedImageRecoverySharesPrimaryLaneLimitAcrossClients(t *testing.T) {
	firstThree := make(chan struct{})
	var started, active, peak atomic.Int32
	var mu sync.Mutex
	counts := make(map[string]int)
	fixture, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(target string, req *http.Request, conn net.Conn) {
		mu.Lock()
		key := req.URL.Path + "|" + target
		counts[key]++
		attempt := counts[key]
		mu.Unlock()
		if started.Add(1) == 3 {
			close(firstThree)
		}
		<-firstThree
		if attempt == 1 {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	})
	dial := dialer.dial
	dialer.dial = func(ctx context.Context, network, target string) (net.Conn, error) {
		count := active.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		conn, err := dial(ctx, network, target)
		if err != nil {
			active.Add(-1)
			return nil, err
		}
		return &bufferedGETTrackedConn{Conn: conn, active: &active}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan error, 3)
	for index := range 3 {
		client := NewBufferedImageGETClient(90 * time.Second)
		client.Transport.(*bufferedGETTransport).base = fixture.Transport.(*bufferedGETTransport).base
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://safe.example/image%d.png", index), nil)
			response, err := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
			}
			results <- err
		}()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 3 || active.Load() != 0 || len(bufferedImageSlots) != 0 || len(dialer.addresses()) > 24 {
		t.Fatalf("recovery exceeded or retained shared resources: peak=%d active=%d permits=%d dials=%v", peak.Load(), active.Load(), len(bufferedImageSlots), dialer.addresses())
	}
	mu.Lock()
	defer mu.Unlock()
	for key, count := range counts {
		if count > 2 {
			t.Errorf("%s attempted %d times, limit two", key, count)
		}
	}
}

func TestBufferedImageRecoveryRejectsEntireUnsafeInitialDNSAnswer(t *testing.T) {
	addresses := append(publicGETAddresses(4), net.IPAddr{IP: net.ParseIP("10.0.0.7")})
	fixture, dialer := bufferedGETFixture(t, addresses, func(string, *http.Request, net.Conn) {
		t.Error("unsafe DNS answer reached a connection")
	})
	response, err := imageRetryFixture(fixture).Get("http://safe.example/image.png")
	if response != nil || err == nil || !strings.Contains(err.Error(), "blocked address") || len(dialer.addresses()) != 0 {
		t.Fatalf("unsafe initial answer bypassed validation: response=%v err=%v dials=%v", response, err, dialer.addresses())
	}
}

func TestBufferedImageRecoveryRetains304AndRedirectWinners(t *testing.T) {
	for _, status := range []string{"304", "redirect"} {
		t.Run(status, func(t *testing.T) {
			var attempts atomic.Int32
			fixture, dialer := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, req *http.Request, conn net.Conn) {
				if attempts.Add(1) == 1 {
					return
				}
				if req.URL.Path == "/next.png" {
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
				} else if status == "304" {
					_, _ = io.WriteString(conn, "HTTP/1.1 304 Not Modified\r\nETag: \"image-v1\"\r\nContent-Length: 20971520\r\n\r\n")
				} else {
					_, _ = io.WriteString(conn, "HTTP/1.1 302 Found\r\nLocation: /next.png\r\nContent-Length: 0\r\n\r\n")
				}
			})
			response, err := imageRetryFixture(fixture).Get("http://safe.example/image.png")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if status == "304" {
				if response.StatusCode != http.StatusNotModified || len(body) != 0 || response.Header.Get("ETag") != `"image-v1"` || len(dialer.addresses()) != 2 {
					t.Fatalf("fresh 304 response changed: status=%d body=%q dials=%v", response.StatusCode, body, dialer.addresses())
				}
			} else if response.StatusCode != http.StatusOK || string(body) != "ok" || response.Request.URL.String() != "http://safe.example/next.png" || len(dialer.addresses()) != 3 {
				t.Fatalf("fresh redirect response changed: status=%d body=%q URL=%v dials=%v", response.StatusCode, body, response.Request.URL, dialer.addresses())
			}
		})
	}
}

func imageRetryFixture(fixture *http.Client) *http.Client {
	client := NewBufferedImageGETClient(90 * time.Second)
	transport := client.Transport.(*bufferedGETTransport)
	transport.base = fixture.Transport.(*bufferedGETTransport).base
	transport.slots = make(chan struct{}, maxGETAddressCandidates)
	transport.imageLanes = newImageCandidateLanes()
	client.CheckRedirect = fixture.CheckRedirect
	return client
}
