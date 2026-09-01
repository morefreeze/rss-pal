package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var syntheticDNSCIDR = func() *net.IPNet {
	_, network, err := net.ParseCIDR("198.18.0.0/15")
	if err != nil {
		panic(err)
	}
	return network
}()

type proxyFallbackResolver struct {
	system  resolver
	trusted resolver
}

type dohResolver struct {
	endpoint string
	client   *http.Client
	mu       sync.Mutex
	cache    map[string]dohCacheEntry
}

type dohCacheEntry struct {
	addresses []net.IPAddr
	expiresAt time.Time
}

const defaultDoHEndpoint = "https://cloudflare-dns.com/dns-query"

const maxDoHCacheEntries = 256

func isSyntheticDNSIP(ip net.IP) bool {
	return ip != nil && syntheticDNSCIDR.Contains(ip)
}

func newProxyDoHResolver(proxy func(*http.Request) (*url.URL, error)) resolver {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxy
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 8 * time.Second
	return &dohResolver{
		endpoint: defaultDoHEndpoint,
		client:   &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}
}

type dohResponse struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func (r *dohResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("DoH resolver is not configured")
	}
	r.mu.Lock()
	if cached, ok := r.cache[host]; ok && time.Now().Before(cached.expiresAt) {
		addresses := append([]net.IPAddr(nil), cached.addresses...)
		r.mu.Unlock()
		return addresses, nil
	}
	r.mu.Unlock()
	endpoint, err := url.Parse(r.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse DoH endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("name", host)
	query.Set("type", "A")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-json")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("DoH lookup: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("DoH lookup returned HTTP status %d", response.StatusCode)
	}
	var payload dohResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode DoH response: %w", err)
	}
	if payload.Status != 0 {
		return nil, fmt.Errorf("DoH lookup returned status %d", payload.Status)
	}
	addresses := make([]net.IPAddr, 0, len(payload.Answer))
	for _, answer := range payload.Answer {
		if answer.Type != 1 {
			continue
		}
		ip := net.ParseIP(answer.Data)
		if ip == nil || isBlockedIP(ip) || isSyntheticDNSIP(ip) {
			return nil, errors.New("DoH lookup returned unsafe address")
		}
		addresses = append(addresses, net.IPAddr{IP: ip})
	}
	if len(addresses) == 0 {
		return nil, errors.New("DoH lookup returned no public A addresses")
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = make(map[string]dohCacheEntry)
	}
	now := time.Now()
	for key, entry := range r.cache {
		if !now.Before(entry.expiresAt) {
			delete(r.cache, key)
		}
	}
	if _, exists := r.cache[host]; !exists && len(r.cache) >= maxDoHCacheEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for key, entry := range r.cache {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = key, entry.expiresAt
			}
		}
		delete(r.cache, oldestKey)
	}
	r.cache[host] = dohCacheEntry{addresses: append([]net.IPAddr(nil), addresses...), expiresAt: now.Add(5 * time.Minute)}
	r.mu.Unlock()
	return addresses, nil
}

func (r proxyFallbackResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	addresses, err := r.system.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if isSyntheticDNSIP(address.IP) {
			return r.trusted.LookupIPAddr(ctx, host)
		}
	}
	return addresses, nil
}
