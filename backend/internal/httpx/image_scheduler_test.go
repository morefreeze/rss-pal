package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// All candidate bodies share one 80 KiB/s link. One representation of the
// three real image sizes fits in 90s; racing two whole copies needlessly spends
// the same shared capacity twice. These are synthetic link parameters, not a
// measurement or attribution of the production timeout.
func TestBufferedImageThreeColdRequestsShareBoundedLink(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sizes := map[string]int{"/01.png": 1170068, "/02.png": 459004, "/03.png": 4195646}
		// A shared pool inside this fake-time bubble has the production four-
		// socket capacity. Outside-bubble channels are not durably blocked to
		// synctest, so using the package global would prevent clock advance.
		slots := make(chan struct{}, maxGETAddressCandidates)
		lanes := newImageCandidateLanes()
		const chunk = 10 << 10
		type transfer struct {
			conn net.Conn
			path string
			body []byte
			done chan error
		}
		jobs := make(chan transfer, 4)
		release := make(chan struct{})
		linkCtx, stopLink := context.WithCancel(context.Background())
		linkDone := make(chan struct{})
		var mu sync.Mutex
		written := make(map[string]int)
		first := make(map[string]time.Duration)
		attempts := make(map[string]int)
		start := time.Now()
		go func() {
			defer close(linkDone)
			<-release
			for {
				select {
				case <-linkCtx.Done():
					return
				case job := <-jobs:
					time.Sleep(125 * time.Millisecond)
					n, err := job.conn.Write(job.body)
					mu.Lock()
					written[job.path] += n
					mu.Unlock()
					job.done <- err
				}
			}
		}()
		addresses := append(publicGETAddresses(2),
			net.IPAddr{IP: net.ParseIP("2606:4700:3035::ac43:adad")},
			net.IPAddr{IP: net.ParseIP("2606:4700:3037::6815:5024")})
		fixture, dialer := bufferedGETFixture(t, addresses, func(target string, req *http.Request, conn net.Conn) {
			path := req.URL.Path
			mu.Lock()
			attempts[path]++
			mu.Unlock()
			if strings.HasPrefix(req.URL.Host, "[") || strings.HasPrefix(target, "[") {
				_, _ = io.WriteString(conn, "HTTP/1.1 500 Proxy Failure\r\nContent-Length: 0\r\n\r\n")
				return
			}
			mu.Lock()
			if _, ok := first[path]; !ok {
				first[path] = time.Since(start)
			}
			mu.Unlock()
			_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: %d\r\n\r\n", sizes[path])
			for remaining := sizes[path]; remaining > 0; {
				n := min(chunk, remaining)
				job := transfer{conn: conn, path: path, body: []byte(strings.Repeat("x", n)), done: make(chan error, 1)}
				select {
				case jobs <- job:
				case <-linkCtx.Done():
					return
				}
				select {
				case err := <-job.done:
					if err != nil {
						return
					}
				case <-linkCtx.Done():
					return
				}
				remaining -= n
			}
		})
		var active, peak atomic.Int32
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
		type result struct {
			Path    string
			Bytes   int
			Elapsed time.Duration
			Error   string
		}
		results := make(chan result, len(sizes))
		ready := make(chan struct{}, len(sizes))
		begin := make(chan struct{})
		for path := range sizes {
			client := imageSchedulerFixture(fixture)
			client.Transport.(*bufferedGETTransport).slots = slots
			client.Transport.(*bufferedGETTransport).imageLanes = lanes
			go func() {
				ready <- struct{}{}
				<-begin
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe.example"+path, nil)
				response, err := client.Do(req)
				got := result{Path: path, Elapsed: time.Since(start)}
				if err != nil {
					got.Error = err.Error()
				} else {
					body, readErr := io.ReadAll(response.Body)
					_ = response.Body.Close()
					got.Bytes = len(body)
					if readErr != nil {
						got.Error = readErr.Error()
					}
				}
				results <- got
			}()
		}
		for range sizes {
			<-ready
		}
		close(begin)
		synctest.Wait() // All requests have arrived before link capacity is released.
		close(release)
		var got []result
		for range sizes {
			got = append(got, <-results)
		}
		stopLink()
		<-linkDone
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		trace, _ := json.Marshal(map[string]any{"results": got, "body_bytes_by_url": written, "first_origin_ns": first, "attempts_by_url": attempts, "socket_peak": peak.Load(), "active_after": active.Load()})
		t.Log(string(trace))
		for _, item := range got {
			if item.Error != "" || item.Bytes != sizes[item.Path] || item.Elapsed >= 90*time.Second {
				t.Errorf("cold image missed the original budget: %+v", item)
			}
		}
		for path, size := range sizes {
			if written[path] != size {
				t.Errorf("healthy progressing %s consumed %d body bytes for one %d-byte image", path, written[path], size)
			}
		}
		if peak.Load() > 4 || active.Load() != 0 {
			t.Errorf("actual socket bound/cleanup: peak=%d active=%d", peak.Load(), active.Load())
		}
	})
}

func TestBufferedImageSchedulerPreservesQuietPrimaryAndLongBody(t *testing.T) {
	for _, scenario := range []string{"31s before headers", "40s progressing body"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fixture, _ := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, req *http.Request, conn net.Conn) {
					if req.URL.Hostname() != "93.184.216.34" {
						_, _ = io.WriteString(conn, "HTTP/1.1 500 Error\r\nContent-Length: 0\r\n\r\n")
						return
					}
					if scenario == "31s before headers" {
						time.Sleep(31 * time.Second)
						_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 40\r\n\r\n"+strings.Repeat("x", 40))
						return
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 40\r\n\r\n")
					for range 40 {
						time.Sleep(time.Second)
						if _, err := io.WriteString(conn, "x"); err != nil {
							return
						}
					}
				})
				client := imageSchedulerFixture(fixture)
				start := time.Now()
				response, err := client.Get("http://safe.example/image.png")
				if err != nil {
					t.Fatalf("legitimate long primary was invalidated: %v", err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				want := 31 * time.Second
				if scenario == "40s progressing body" {
					want = 40 * time.Second
				}
				if err != nil || len(body) != 40 || time.Since(start) != want {
					t.Fatalf("long primary: body=%d err=%v elapsed=%v", len(body), err, time.Since(start))
				}
			})
		})
	}
}

// This is a no-regression control: eager address racing already finds the
// third healthy peer. A two-candidate cap must not strand that peer at 90s.
func TestBufferedImageSchedulerReachesThirdPeerWithoutDiscardingPrimary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closed atomic.Int32
		var hanging atomic.Int32
		bothHanging := make(chan struct{})
		fixture, _ := bufferedGETFixture(t, publicGETAddresses(3), func(_ string, req *http.Request, conn net.Conn) {
			if req.URL.Hostname() == "93.184.216.34" || req.URL.Hostname() == "8.8.8.8" {
				if hanging.Add(1) == 2 {
					close(bothHanging)
				}
				_, _ = io.Copy(io.Discard, conn)
				closed.Add(1)
				return
			}
			<-bothHanging
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		})
		client := imageSchedulerFixture(fixture)
		start := time.Now()
		response, err := client.Get("http://safe.example/image.png")
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		synctest.Wait()
		if time.Since(start) >= 10*time.Second || closed.Load() != 2 || len(client.Transport.(*bufferedGETTransport).slots) != 0 {
			t.Fatalf("healthy third peer was stranded or losers leaked: elapsed=%v closed=%d permits=%d", time.Since(start), closed.Load(), len(client.Transport.(*bufferedGETTransport).slots))
		}
	})
}

func TestBufferedImageSchedulerPreservesQuietNonLastHealthyAlternative(t *testing.T) {
	for _, phase := range []string{"3s before headers", "31s silent body"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var mu sync.Mutex
				calls := make(map[string]int)
				fixture, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, req *http.Request, conn net.Conn) {
					target := req.URL.Hostname()
					mu.Lock()
					calls[target]++
					mu.Unlock()
					switch target {
					case "93.184.216.34":
						_, _ = io.Copy(io.Discard, conn)
					case "8.8.8.8":
						if phase == "3s before headers" {
							time.Sleep(3 * time.Second)
						}
						if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n"); err != nil {
							return
						}
						if phase == "31s silent body" {
							time.Sleep(31 * time.Second)
						}
						_, _ = io.WriteString(conn, "ok")
					default:
						_, _ = io.WriteString(conn, "HTTP/1.1 500 Error\r\nContent-Length: 0\r\n\r\n")
					}
				})
				client := imageSchedulerFixture(fixture)
				start := time.Now()
				response, err := client.Get("http://safe.example/image.png")
				if err != nil {
					t.Fatalf("the only healthy nonlast peer was discarded: %v", err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || string(body) != "ok" || time.Since(start) >= 90*time.Second || len(dialer.addresses()) > 8 {
					t.Fatalf("healthy alternative: body=%q err=%v elapsed=%v dials=%v", body, err, time.Since(start), dialer.addresses())
				}
				mu.Lock()
				defer mu.Unlock()
				for target, count := range calls {
					if count > 2 {
						t.Errorf("%s attempted %d times, maximum two including scheduler resumes", target, count)
					}
				}
			})
		})
	}
}

func TestBufferedImageSchedulerSharesLanesAndCancelsQueuedWork(t *testing.T) {
	first := NewBufferedImageGETClient(time.Minute).Transport.(*bufferedGETTransport)
	second := NewBufferedImageGETClient(time.Minute).Transport.(*bufferedGETTransport)
	if first.slots != second.slots || first.imageLanes != second.imageLanes || cap(first.imageLanes.primary) != 3 || cap(first.imageLanes.secondary) != 1 {
		t.Fatal("production image clients must share three primary and one secondary lane")
	}
	synctest.Test(t, func(t *testing.T) {
		slots, lanes := make(chan struct{}, 4), newImageCandidateLanes()
		fixture, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, _ *http.Request, conn net.Conn) {
			_, _ = io.Copy(io.Discard, conn)
		})
		var active, peak atomic.Int32
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
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 4)
		for index := range 4 {
			client := imageSchedulerFixture(fixture)
			transport := client.Transport.(*bufferedGETTransport)
			transport.slots, transport.imageLanes = slots, lanes
			go func() {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://safe.example/image%d.png", index), nil)
				_, err := client.Do(req)
				done <- err
			}()
		}
		synctest.Wait()
		if len(dialer.addresses()) != 3 || len(slots) != 3 || len(lanes.primary) != 3 || len(lanes.secondary) != 0 {
			t.Fatalf("queued primary owns a physical permit: dials=%v global=%d primary=%d secondary=%d", dialer.addresses(), len(slots), len(lanes.primary), len(lanes.secondary))
		}
		time.Sleep(imageHedgeIdle)
		synctest.Wait()
		if len(dialer.addresses()) != 4 || len(slots) != 4 || len(lanes.secondary) != 1 || peak.Load() != 4 {
			t.Fatalf("queued hedges exceeded the spare lane: dials=%v global=%d secondary=%d peak=%d", dialer.addresses(), len(slots), len(lanes.secondary), peak.Load())
		}
		cancel()
		for range 4 {
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("caller cancellation changed: %v", err)
			}
		}
		synctest.Wait()
		if active.Load() != 0 || len(slots) != 0 || len(lanes.primary) != 0 || len(lanes.secondary) != 0 {
			t.Fatalf("cancel retained resources: active=%d global=%d primary=%d secondary=%d", active.Load(), len(slots), len(lanes.primary), len(lanes.secondary))
		}
	})
}

func TestBufferedImageSchedulerPromotionReleasesSecondaryLane(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var third atomic.Int32
		fixture, _ := bufferedGETFixture(t, publicGETAddresses(3), func(_ string, req *http.Request, conn net.Conn) {
			switch req.URL.Hostname() {
			case "93.184.216.34":
				time.Sleep(3 * time.Second)
			case "8.8.8.8":
				if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n"); err != nil {
					return
				}
				time.Sleep(5 * time.Second)
				_, _ = io.WriteString(conn, "ok")
			default:
				third.Add(1)
				_, _ = io.WriteString(conn, "HTTP/1.1 500 Error\r\nContent-Length: 0\r\n\r\n")
			}
		})
		client := imageSchedulerFixture(fixture)
		response, err := client.Get("http://safe.example/image.png")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		transport := client.Transport.(*bufferedGETTransport)
		if err != nil || string(body) != "ok" || third.Load() != 1 || len(transport.slots) != 0 || len(transport.imageLanes.secondary) != 0 {
			t.Fatalf("promoted hedge blocked its successor or leaked: body=%q err=%v third=%d global=%d secondary=%d", body, err, third.Load(), len(transport.slots), len(transport.imageLanes.secondary))
		}
	})
}

func TestBufferedImageSchedulerYieldAndEOFShareTwoAttemptBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var alternative atomic.Int32
		fixture, dialer := bufferedGETFixture(t, publicGETAddresses(4), func(_ string, req *http.Request, conn net.Conn) {
			switch req.URL.Hostname() {
			case "93.184.216.34":
				_, _ = io.Copy(io.Discard, conn)
			case "8.8.8.8":
				if alternative.Add(1) == 1 {
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n")
					_, _ = io.Copy(io.Discard, conn) // Scheduler-owned idle yield.
				}
				// The resumed attempt returns EOF, not permission for a third.
			default:
				_, _ = io.WriteString(conn, "HTTP/1.1 500 Error\r\nContent-Length: 0\r\n\r\n")
			}
		})
		client := imageSchedulerFixture(fixture)
		response, err := client.Get("http://safe.example/image.png")
		if response != nil || !errors.Is(err, context.DeadlineExceeded) || alternative.Load() != 2 || len(dialer.addresses()) > 8 || len(client.Transport.(*bufferedGETTransport).slots) != 0 {
			t.Fatalf("yield/EOF exceeded the shared allowance: response=%v err=%v alternative=%d dials=%v", response, err, alternative.Load(), dialer.addresses())
		}
	})
}

func TestBufferedImageSchedulerRotatesRealProxyStalls(t *testing.T) {
	for _, phase := range []string{"CONNECT", "TLS", "headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			certificate, roots := testCertificate(t)
			certificateTime := time.Now()
			synctest.Test(t, func(t *testing.T) {
				var hanging, closed atomic.Int32
				bothHanging := make(chan struct{})
				fixture, _ := bufferedGETFixture(t, publicGETAddresses(3), func(_ string, connect *http.Request, conn net.Conn) {
					stalled := connect.Host != "1.1.1.1:443"
					block := func(wire net.Conn) {
						if hanging.Add(1) == 2 {
							close(bothHanging)
						}
						_, _ = io.Copy(io.Discard, wire)
						closed.Add(1)
					}
					if stalled && phase == "CONNECT" {
						block(conn)
						return
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					if stalled && phase == "TLS" {
						header := make([]byte, 5)
						if _, err := io.ReadFull(conn, header); err == nil {
							_, _ = io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint16(header[3:5])))
						}
						block(conn)
						return
					}
					origin := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
					if err := origin.Handshake(); err != nil {
						return
					}
					if _, err := http.ReadRequest(bufio.NewReader(origin)); err != nil {
						return
					}
					if stalled {
						if phase == "body" {
							_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nprefix")
						}
						block(origin)
						return
					}
					<-bothHanging
					_, _ = io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
				})
				client := imageRetryFixture(fixture)
				transport := client.Transport.(*bufferedGETTransport)
				proxyURL, _ := url.Parse("http://proxy.example:8888")
				transport.base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
				transport.base.tlsConfig = &tls.Config{RootCAs: roots, Time: func() time.Time { return certificateTime }}
				start := time.Now()
				response, err := client.Get("https://safe.example/image.png")
				if err != nil {
					t.Fatalf("two stalled %s peers hid a healthy third: %v", phase, err)
				}
				_ = response.Body.Close()
				synctest.Wait()
				if time.Since(start) >= 10*time.Second || closed.Load() != 2 || len(transport.slots) != 0 {
					t.Fatalf("stall rotation leaked or stranded a peer: elapsed=%v closed=%d permits=%d", time.Since(start), closed.Load(), len(transport.slots))
				}
			})
		})
	}
}

func imageSchedulerFixture(fixture *http.Client) *http.Client {
	client := imageRetryFixture(fixture)
	proxyURL, _ := url.Parse("http://proxy.example:8888")
	transport := client.Transport.(*bufferedGETTransport)
	base := *transport.base
	base.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	transport.base = &base
	return client
}
