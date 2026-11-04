package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
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

func TestBufferedGETConditional304WinsWithoutReadingEntityLength(t *testing.T) {
	for _, length := range []int{0, 20 << 20} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := make(chan struct{})
				closed := make(chan struct{})
				client, _ := bufferedGETFixture(t, publicGETAddresses(2), func(address string, req *http.Request, conn net.Conn) {
					if req.Header.Get("If-None-Match") != `"feed-v1"` || req.Header.Get("If-Modified-Since") != "Wed, 04 Nov 2026 00:00:00 GMT" {
						t.Error("conditional feed headers changed")
					}
					if strings.HasPrefix(address, "8.8.8.8:") {
						close(started)
						_, _ = io.Copy(io.Discard, conn)
						close(closed)
						return
					}
					<-started
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 304 Not Modified\r\nContent-Length: %d\r\nETag: \"feed-v1\"\r\n\r\n", length)
				})
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/feed.xml", nil)
				req.Header.Set("If-None-Match", `"feed-v1"`)
				req.Header.Set("If-Modified-Since", "Wed, 04 Nov 2026 00:00:00 GMT")
				response, err := client.Do(req)
				if err != nil {
					t.Fatalf("fast 304 lost to hanging address: %v", err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || len(body) != 0 || response.StatusCode != http.StatusNotModified || response.Header.Get("Content-Length") != fmt.Sprint(length) || response.Header.Get("ETag") != `"feed-v1"` || ctx.Err() != nil {
					t.Fatalf("conditional response: status=%d body=%q err=%v context=%v", response.StatusCode, body, err, ctx.Err())
				}
				<-closed
			})
		})
	}
}

func TestBufferedGETRecoversFromEOFAndFailedCONNECT(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		t.Run(fmt.Sprint(proxy), func(t *testing.T) {
			failed := make(chan struct{})
			// With an HTTP target the fixture sees the pinned absolute URL.
			// CONNECT failure itself is exercised below at the TLS boundary.
			client, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(address string, req *http.Request, conn net.Conn) {
				if strings.HasPrefix(address, "93.184.216.34:") || req.URL.Hostname() == "93.184.216.34" {
					close(failed)
					return // EOF before response headers
				}
				<-failed
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
			})
			if proxy {
				proxyURL, _ := url.Parse("http://proxy.example:8888")
				client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
			}
			response, err := client.Get("http://safe.example/feed.xml")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(body) != "ok" || len(dialer.addresses()) != 2 {
				t.Fatalf("EOF recovery: body=%q err=%v dials=%v", body, err, dialer.addresses())
			}
		})
	}

	client, _ := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, req *http.Request, conn net.Conn) {
		if req.Method != http.MethodConnect {
			t.Error("expected CONNECT")
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 500 Proxy Failure\r\nContent-Length: 0\r\n\r\n")
	})
	proxyURL, _ := url.Parse("http://proxy.example:8888")
	client.Transport.(*bufferedGETTransport).base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	if _, err := client.Get("https://safe.example/feed.xml"); err == nil || !strings.Contains(err.Error(), "CONNECT returned HTTP status 500") {
		t.Fatalf("failed CONNECT returned success: %v", err)
	}
}

func TestBufferedGETFailedCONNECTRecoversThroughCheckedPeer(t *testing.T) {
	certificate, roots := testCertificate(t)
	failed := make(chan struct{})
	client, dialer := bufferedGETFixture(t, publicGETAddresses(2), func(_ string, req *http.Request, conn net.Conn) {
		if req.Method != http.MethodConnect {
			t.Error("expected CONNECT")
			return
		}
		if req.Host == "93.184.216.34:443" {
			_, _ = io.WriteString(conn, "HTTP/1.1 500 Proxy Failure\r\nContent-Length: 0\r\n\r\n")
			close(failed)
			return
		}
		<-failed
		if req.Host != "8.8.8.8:443" {
			t.Errorf("unvalidated proxy target %q", req.Host)
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		origin := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
		if err := origin.Handshake(); err != nil {
			t.Error(err)
			return
		}
		request, err := http.ReadRequest(bufio.NewReader(origin))
		if err != nil {
			t.Error(err)
			return
		}
		if request.Host != "safe.example" || origin.ConnectionState().ServerName != "safe.example" {
			t.Error("proxy recovery lost public Host/SNI")
		}
		_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	})
	proxyURL, _ := url.Parse("http://proxy.example:8888")
	transport := client.Transport.(*bufferedGETTransport)
	transport.base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	transport.base.tlsConfig = &tls.Config{RootCAs: roots}
	response, err := client.Get("https://safe.example/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(body) != "ok" || len(dialer.addresses()) != 2 || len(transport.slots) != 0 {
		t.Fatalf("CONNECT recovery body=%q err=%v dials=%v permits=%d", body, err, dialer.addresses(), len(transport.slots))
	}
}

// A saturated image workload must not consume the permits needed by public
// feed preview or manual HTML captures. No real addresses are contacted.
func TestBufferedImageWorkloadDoesNotStarveHTMLAndFeeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		image := NewBufferedImageGETClient(time.Minute)
		imageTransport := image.Transport.(*bufferedGETTransport)
		manual := NewBufferedGETClient(time.Second).Transport.(*bufferedGETTransport)
		if imageTransport.slots == manual.slots || cap(imageTransport.slots) != 4 || cap(manual.slots) != 4 {
			t.Fatal("image and HTML/feed workloads need separate bounded permits")
		}
		var closed atomic.Int32
		fixture, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, _ *http.Request, conn net.Conn) {
			_, _ = io.Copy(io.Discard, conn)
			closed.Add(1)
		})
		imageTransport.base = fixture.Transport.(*bufferedGETTransport).base
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example/image.png", nil)
			_, err := image.Do(req)
			done <- err
		}()
		synctest.Wait()
		if len(dialer.addresses()) != 4 || len(imageTransport.slots) != 4 {
			t.Fatal("image fixture did not saturate its four permits")
		}
		feed, _ := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		})
		response, err := feed.Get("http://safe.example/feed.xml")
		if err != nil {
			t.Errorf("saturated image work blocked feed: %v", err)
		} else {
			_ = response.Body.Close()
		}
		cancel()
		<-done
		synctest.Wait()
		if closed.Load() != 4 || len(imageTransport.slots) != 0 {
			t.Errorf("cancelled images retained sockets=%d permits=%d", 4-closed.Load(), len(imageTransport.slots))
		}
	})
}

func TestBufferedImageBodyLimitKeepsHTMLDefault(t *testing.T) {
	for _, size := range []int{8 << 20, (8 << 20) + 1, 10 << 20, (10 << 20) + 1} {
		for _, image := range []bool{false, true} {
			t.Run(fmt.Sprintf("bytes=%d/image=%t", size, image), func(t *testing.T) {
				fixture, _ := bufferedGETFixture(t, publicGETAddresses(1), func(_ string, _ *http.Request, conn net.Conn) {
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", size)
					_, _ = io.WriteString(conn, strings.Repeat("x", size))
				})
				client := NewBufferedGETClient(time.Second)
				limit := 8 << 20
				if image {
					client = NewBufferedImageGETClient(time.Second)
					limit = 10 << 20
				}
				client.Transport.(*bufferedGETTransport).base = fixture.Transport.(*bufferedGETTransport).base
				response, err := client.Get("http://safe.example/body")
				if size > limit {
					if err == nil {
						response.Body.Close()
						t.Fatal("oversize body accepted")
					}
					return
				}
				if err != nil {
					t.Fatalf("bounded complete body rejected: %v", err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || len(body) != size {
					t.Fatalf("body bytes=%d err=%v", len(body), err)
				}
			})
		}
	}
}
