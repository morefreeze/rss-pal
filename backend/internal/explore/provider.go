// Package explore discovers public candidate sources. It deliberately never
// marks a candidate as valid; Task4 owns validation and content ingestion.
package explore

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/util"
)

// Candidate is public provider metadata for a possible feed source.
type Candidate struct {
	ExternalKey     string
	FeedURL         string
	SiteURL         string
	Title           string
	Topic           string
	Tags            []string
	OccurrenceCount int
}

// Provider is the public registry configuration needed by an adapter.
type Provider struct {
	ID       int
	Key      string
	Kind     string
	Endpoint string
	Topic    string
}

// ProviderAdapter turns one provider document into public candidate sources.
type ProviderAdapter interface {
	Kind() string
	Parse(Provider, []byte) ([]Candidate, error)
}

const (
	defaultProviderBodyBytes = 4 << 20
	providerUserAgent        = "rss-pal-explore/1.0"
)

// ProviderClient safely fetches public registry documents. Tests can inject a
// client and validator; production defaults retain the shared httpx defenses.
type ProviderClient struct {
	doer interface {
		Do(*http.Request) (*http.Response, error)
	}
	validateURL   func(string) (*url.URL, error)
	rssHubBaseURL string
	maxBodyBytes  int64
	userAgent     string
}

type ProviderFetchResult struct {
	Body         []byte
	NotModified  bool
	ETag         string
	LastModified string
}

func NewProviderClient(rssHubBaseURL string) ProviderClient {
	return ProviderClient{doer: httpx.NewClient(20 * time.Second), validateURL: httpx.ValidateURL, rssHubBaseURL: rssHubBaseURL, maxBodyBytes: defaultProviderBodyBytes, userAgent: providerUserAgent}
}

func (c ProviderClient) Fetch(ctx context.Context, endpoint, etag, lastModified string) (ProviderFetchResult, error) {
	endpoint, err := c.resolveEndpoint(endpoint)
	if err != nil {
		return ProviderFetchResult{}, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ProviderFetchResult{}, fmt.Errorf("invalid provider endpoint")
	}
	validate := c.validateURL
	if validate == nil {
		validate = httpx.ValidateURL
	}
	if _, err := validate(endpoint); err != nil {
		return ProviderFetchResult{}, fmt.Errorf("validate provider endpoint: %w", err)
	}
	client := c.doer
	if client == nil {
		client = httpx.NewClient(20 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProviderFetchResult{}, err
	}
	userAgent := c.userAgent
	if userAgent == "" {
		userAgent = providerUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	response, err := client.Do(req)
	if err != nil {
		return ProviderFetchResult{}, err
	}
	defer response.Body.Close()
	result := ProviderFetchResult{NotModified: response.StatusCode == http.StatusNotModified, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified")}
	if result.NotModified {
		return result, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ProviderFetchResult{}, fmt.Errorf("provider response status %d", response.StatusCode)
	}
	limit := c.maxBodyBytes
	if limit <= 0 {
		limit = defaultProviderBodyBytes
	}
	result.Body, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return ProviderFetchResult{}, err
	}
	if int64(len(result.Body)) > limit {
		return ProviderFetchResult{}, fmt.Errorf("provider response exceeds %d bytes", limit)
	}
	return result, nil
}

func (c ProviderClient) resolveEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("parse provider endpoint: %w", err)
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	if !strings.HasPrefix(endpoint, "/") || c.rssHubBaseURL == "" {
		return "", fmt.Errorf("relative provider endpoint requires RSSHub base URL")
	}
	base, err := url.Parse(c.rssHubBaseURL)
	if err != nil || !base.IsAbs() {
		return "", fmt.Errorf("invalid RSSHub base URL")
	}
	return base.ResolveReference(u).String(), nil
}

// NormalizeCandidates rejects unsafe URLs and coalesces candidates by their
// canonical feed URL. The lexically smallest stable key wins public metadata.
func NormalizeCandidates(input []Candidate) []Candidate {
	byURL := make(map[string]Candidate, len(input))
	for _, raw := range input {
		feedURL, ok := normalizePublicURL(raw.FeedURL)
		if !ok {
			continue
		}
		raw.FeedURL = feedURL
		if siteURL, ok := normalizePublicURL(raw.SiteURL); ok {
			raw.SiteURL = siteURL
		} else {
			raw.SiteURL = ""
		}
		if raw.OccurrenceCount < 1 {
			raw.OccurrenceCount = 1
		}
		if previous, exists := byURL[feedURL]; exists {
			previous.OccurrenceCount += raw.OccurrenceCount
			previous.Tags = uniqueStrings(append(previous.Tags, raw.Tags...))
			if raw.ExternalKey < previous.ExternalKey {
				previous.ExternalKey = raw.ExternalKey
				if raw.Title != "" {
					previous.Title = raw.Title
				}
				if raw.SiteURL != "" {
					previous.SiteURL = raw.SiteURL
				}
				if raw.Topic != "" {
					previous.Topic = raw.Topic
				}
			}
			byURL[feedURL] = previous
			continue
		}
		raw.Tags = uniqueStrings(raw.Tags)
		byURL[feedURL] = raw
	}
	out := make([]Candidate, 0, len(byURL))
	for _, c := range byURL {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FeedURL == out[j].FeedURL {
			return out[i].ExternalKey < out[j].ExternalKey
		}
		return out[i].FeedURL < out[j].FeedURL
	})
	return out
}

func normalizePublicURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", false
	}
	if isPrivateHost(u.Hostname()) {
		return "", false
	}
	return util.NormalizeURL(u.String()), true
}

func isPrivateHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

func uniqueStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
