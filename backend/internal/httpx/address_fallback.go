package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxGETAddressCandidates = 4
	bufferedGETBodyLimit    = 8 << 20
	bufferedImageBodyLimit  = 10 << 20
)

// Bound candidate connections across clients and concurrent HTML/feed fetches.
var bufferedGETSlots = make(chan struct{}, maxGETAddressCandidates)

// Large, slow image transfers must not occupy the permits needed by interactive
// HTML captures and subscription previews. Each workload has four active
// candidates across all its clients, rather than four per caller.
var bufferedImageSlots = make(chan struct{}, maxGETAddressCandidates)

// NewBufferedGETClient is an opt-in client for public HTML captures and feeds.
// It races at most four checked addresses and buffers a complete, bounded
// response before choosing a winner. Other methods and authenticated requests
// retain NewClient's single-address, streaming behavior.
func NewBufferedGETClient(timeout time.Duration) *http.Client {
	return withBufferedGETAddressFallback(NewClient(timeout))
}

// NewBufferedImageGETClient applies the same checked-address recovery with a
// 10 MiB body limit and a separate bounded image workload. It starts one
// protected primary, adding a secondary after inactivity rather than eagerly
// copying every body. EOF/reset recovery and scheduler-yield resumes share at
// most two attempts per checked IP within the original request's deadline.
func NewBufferedImageGETClient(timeout time.Duration) *http.Client {
	client := NewClient(timeout)
	client.Transport = &bufferedGETTransport{base: client.Transport.(*safeRoundTripper), slots: bufferedImageSlots, bodyLimit: bufferedImageBodyLimit, retryPreBodyFailures: true, imageLanes: bufferedImageLanes}
	return client
}

func withBufferedGETAddressFallback(client *http.Client) *http.Client {
	client.Transport = &bufferedGETTransport{base: client.Transport.(*safeRoundTripper), slots: bufferedGETSlots, bodyLimit: bufferedGETBodyLimit}
	return client
}

type bufferedGETTransport struct {
	base                 *safeRoundTripper
	slots                chan struct{}
	bodyLimit            int64
	retryPreBodyFailures bool
	imageLanes           *imageCandidateLanes
}

type bufferedGETResult struct {
	response *http.Response
	err      error
	address  net.IPAddr
}

// Only failures before response headers can start a fresh image attempt. Body
// truncation, size limits, rejected redirects and HTTP errors are not retried.
type bufferedGETPreBodyError struct{ error }

func (e *bufferedGETPreBodyError) Unwrap() error { return e.error }

func transientPreBodyFailure(err error) bool {
	var preBody *bufferedGETPreBodyError
	return errors.As(err, &preBody) && (errors.Is(preBody, io.EOF) || errors.Is(preBody, io.ErrUnexpectedEOF) || errors.Is(preBody, syscall.ECONNRESET))
}

func (t *bufferedGETTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || (req.Body != nil && req.Body != http.NoBody) ||
		req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
		return t.base.RoundTrip(req)
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	addresses, err := t.addresses(req)
	if err != nil {
		return nil, err
	}
	if t.retryPreBodyFailures {
		return t.roundTripImages(req, addresses)
	}
	response, _, err := t.roundTripCandidates(req, addresses)
	if contextErr := req.Context().Err(); contextErr != nil {
		return nil, contextErr
	}
	return response, err
}

func (t *bufferedGETTransport) roundTripCandidates(req *http.Request, addresses []net.IPAddr) (*http.Response, []net.IPAddr, error) {
	ctx, cancel := context.WithCancel(req.Context())
	results := make(chan bufferedGETResult, len(addresses))
	var attempts sync.WaitGroup
	defer func() {
		cancel()
		attempts.Wait()
	}()
	for _, address := range addresses {
		attempts.Add(1)
		go func(address net.IPAddr) {
			defer attempts.Done()
			select {
			case t.slots <- struct{}{}:
				defer func() { <-t.slots }()
			case <-ctx.Done():
				results <- bufferedGETResult{err: ctx.Err()}
				return
			}
			if err := ctx.Err(); err != nil {
				results <- bufferedGETResult{err: err}
				return
			}
			response, err := t.fetchCandidate(req.Clone(ctx), address, nil)
			results <- bufferedGETResult{response: response, err: err, address: address}
		}(address)
	}
	var firstResponse *http.Response
	var failures []error
	var retryAddresses []net.IPAddr
	for range addresses {
		select {
		case <-req.Context().Done():
			return nil, nil, req.Context().Err()
		case result := <-results:
			if err := req.Context().Err(); err != nil {
				return nil, nil, err
			}
			if result.err != nil {
				failures = append(failures, result.err)
				if transientPreBodyFailure(result.err) {
					retryAddresses = append(retryAddresses, result.address)
				}
				continue
			}
			result.response.Request = req
			if bufferedGETWinner(result.response) {
				return result.response, nil, nil
			}
			// A quick CDN error must not beat a complete 200 from another IP.
			// If every address fails, retain a complete HTTP error for the caller.
			if firstResponse == nil {
				firstResponse = result.response
			}
		}
	}
	if firstResponse != nil {
		return firstResponse, retryAddresses, nil
	}
	return nil, retryAddresses, fmt.Errorf("all public addresses failed: %w", errors.Join(failures...))
}

func bufferedGETWinner(response *http.Response) bool {
	return response != nil && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNotModified || bufferedGETRedirect(response))
}

func (t *bufferedGETTransport) addresses(req *http.Request) ([]net.IPAddr, error) {
	parsed, err := validateURL(req.Context(), req.URL.String(), t.base.resolver)
	if err != nil {
		return nil, err
	}
	var addresses []net.IPAddr
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		addresses = []net.IPAddr{{IP: ip}}
	} else {
		addresses, err = t.base.resolver.LookupIPAddr(req.Context(), parsed.Hostname())
		if err != nil {
			return nil, fmt.Errorf("dns: %w", err)
		}
	}
	seen := make(map[string]bool)
	candidates := make([]net.IPAddr, 0, maxGETAddressCandidates)
	for _, address := range addresses {
		// Validate the entire answer, including addresses beyond the race cap.
		if address.IP == nil || isBlockedIP(address.IP) || isSyntheticDNSIP(address.IP) {
			return nil, errors.New("blocked address")
		}
		key := address.IP.String()
		if !seen[key] && len(candidates) < maxGETAddressCandidates {
			candidates = append(candidates, address)
			seen[key] = true
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("dns: no addresses")
	}
	return candidates, nil
}

func (t *bufferedGETTransport) fetchCandidate(req *http.Request, address net.IPAddr, progress func()) (*http.Response, error) {
	selected := candidateResolver{host: req.URL.Hostname(), address: address, fallback: t.base.resolver}
	dialCtx, cancelDial := context.WithCancel(req.Context())
	connections := &candidateDialer{ctx: dialCtx, cancel: cancelDial, base: t.base.dialer, progress: progress}
	defer connections.close()
	transport := t.base.direct.Clone()
	// This transport serves one candidate only. Tie even its connection dial
	// to the attempt context, rather than net/http's detached pooling context.
	transport.DialContext = func(_ context.Context, network, target string) (net.Conn, error) {
		return safeDialContext(selected, connections)(req.Context(), network, target)
	}
	defer transport.CloseIdleConnections()
	base := *t.base
	base.direct = transport
	base.resolver = selected
	base.dialer = connections
	response, err := base.RoundTrip(req)
	if err != nil {
		return nil, &bufferedGETPreBodyError{err}
	}
	defer closeBufferedGETBody(response.Body)
	// A 304 has no message body. Its Content-Length may describe the complete
	// cached representation, which is neither downloaded nor buffered here.
	if response.StatusCode == http.StatusNotModified {
		response.Body = http.NoBody
		return response, nil
	}
	if response.ContentLength > t.bodyLimit {
		return nil, fmt.Errorf("response exceeds %d bytes", t.bodyLimit)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, t.bodyLimit+1))
	if err != nil {
		if progress != nil && req.Context().Err() != nil {
			// Keep a scheduler-owned cancellation distinct from an upstream
			// body failure, even if closing its socket reports EOF/ErrClosed.
			err = req.Context().Err()
		}
		return nil, fmt.Errorf("read complete response: %w", err)
	}
	if int64(len(body)) > t.bodyLimit {
		return nil, fmt.Errorf("response exceeds %d bytes", t.bodyLimit)
	}
	if bufferedGETRedirect(response) {
		// Resolve against the public source URL, before any candidate can win
		// and cancel a usable 200. Client.CheckRedirect still checks each hop
		// and the redirect count when the client follows this response.
		target, err := req.URL.Parse(response.Header.Get("Location"))
		if err != nil {
			return nil, fmt.Errorf("redirect rejected: %w", err)
		}
		redirect := req.Clone(req.Context())
		redirect.URL = target
		if _, err := t.addresses(redirect); err != nil {
			return nil, fmt.Errorf("redirect rejected: %w", err)
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

// candidateDialer owns the actual sockets, including sockets created by an
// inner SOCKS transport. Its lifetime ends before the candidate permit is freed.
type candidateDialer struct {
	ctx      context.Context
	cancel   context.CancelFunc
	base     dialer
	mu       sync.Mutex
	closed   bool
	conns    []net.Conn
	pending  sync.WaitGroup
	progress func()
}

func (d *candidateDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	if d.closed || d.ctx.Err() != nil {
		d.mu.Unlock()
		if err := d.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	d.pending.Add(1)
	d.mu.Unlock()
	defer d.pending.Done()
	conn, err := d.base.DialContext(d.ctx, network, address)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}
	if d.progress != nil {
		conn = &imageProgressConn{Conn: conn, progress: d.progress}
		d.progress()
	}
	d.mu.Lock()
	if d.closed || d.ctx.Err() != nil {
		d.mu.Unlock()
		_ = conn.Close()
		if err := d.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	d.conns = append(d.conns, conn)
	d.mu.Unlock()
	return conn, nil
}

func (d *candidateDialer) close() {
	d.cancel()
	d.mu.Lock()
	d.closed = true
	connections := d.conns
	d.conns = nil
	d.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	d.pending.Wait()
}

func closeBufferedGETBody(body io.ReadCloser) {
	if proxied, ok := body.(*closeOnClose); ok {
		// A rejected or cancelled candidate must not drain an unbounded proxy
		// body. Close this attempt's connection before the HTTP body wrapper.
		_ = proxied.conn.Close()
	}
	_ = body.Close()
}

type candidateResolver struct {
	host     string
	address  net.IPAddr
	fallback resolver
}

func (r candidateResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if strings.EqualFold(host, r.host) {
		return []net.IPAddr{r.address}, nil
	}
	return r.fallback.LookupIPAddr(ctx, host)
}

func bufferedGETRedirect(response *http.Response) bool {
	if response.Header.Get("Location") == "" {
		return false
	}
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}
