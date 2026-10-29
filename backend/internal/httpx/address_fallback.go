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
	"time"
)

const (
	maxGETAddressCandidates = 4
	bufferedGETBodyLimit    = 8 << 20
)

// Bound candidate connections across clients and concurrent manual captures.
var bufferedGETSlots = make(chan struct{}, maxGETAddressCandidates)

// NewBufferedGETClient is an opt-in client for manual public HTML captures.
// It races at most four checked addresses and buffers a complete, bounded
// response before choosing a winner. Other methods and authenticated requests
// retain NewClient's single-address, streaming behavior.
func NewBufferedGETClient(timeout time.Duration) *http.Client {
	return withBufferedGETAddressFallback(NewClient(timeout))
}

func withBufferedGETAddressFallback(client *http.Client) *http.Client {
	client.Transport = &bufferedGETTransport{base: client.Transport.(*safeRoundTripper), slots: bufferedGETSlots}
	return client
}

type bufferedGETTransport struct {
	base  *safeRoundTripper
	slots chan struct{}
}

type bufferedGETResult struct {
	response *http.Response
	err      error
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
			response, err := t.fetchCandidate(req.Clone(ctx), address)
			results <- bufferedGETResult{response: response, err: err}
		}(address)
	}
	var firstResponse *http.Response
	var failures []error
	for range addresses {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case result := <-results:
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			if result.err != nil {
				failures = append(failures, result.err)
				continue
			}
			result.response.Request = req
			if result.response.StatusCode == http.StatusOK || bufferedGETRedirect(result.response) {
				return result.response, nil
			}
			// A quick CDN error must not beat a complete 200 from another IP.
			// If every address fails, retain a complete HTTP error for the caller.
			if firstResponse == nil {
				firstResponse = result.response
			}
		}
	}
	if firstResponse != nil {
		return firstResponse, nil
	}
	return nil, fmt.Errorf("all public addresses failed: %w", errors.Join(failures...))
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

func (t *bufferedGETTransport) fetchCandidate(req *http.Request, address net.IPAddr) (*http.Response, error) {
	selected := candidateResolver{host: req.URL.Hostname(), address: address, fallback: t.base.resolver}
	transport := t.base.direct.Clone()
	// This transport serves one candidate only. Tie even its connection dial
	// to the attempt context, rather than net/http's detached pooling context.
	transport.DialContext = func(_ context.Context, network, target string) (net.Conn, error) {
		return safeDialContext(selected, t.base.dialer)(req.Context(), network, target)
	}
	defer transport.CloseIdleConnections()
	base := *t.base
	base.direct = transport
	base.resolver = selected
	response, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer closeBufferedGETBody(response.Body)
	if response.ContentLength > bufferedGETBodyLimit {
		return nil, fmt.Errorf("HTML response exceeds %d bytes", bufferedGETBodyLimit)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, bufferedGETBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read complete HTML response: %w", err)
	}
	if len(body) > bufferedGETBodyLimit {
		return nil, fmt.Errorf("HTML response exceeds %d bytes", bufferedGETBodyLimit)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
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
