// Package explore discovers public candidate sources. It deliberately never
// marks a candidate as valid; Task4 owns validation and content ingestion.
package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	maxCandidateFeedURLBytes = 2048
	maxCandidateSiteURLBytes = 2048
	maxCandidateTitleBytes   = 500
	maxCandidateTopicBytes   = 100
	maxCandidateTagCount     = 20
	maxCandidateKeyBytes     = 500
	maxProviderCandidates    = 2000
	maxCandidateOccurrence   = 2147483647
)

// ProviderClient safely fetches public registry documents. Tests can inject a
// client and validator; production defaults retain the shared httpx defenses.
type ProviderClient struct {
	publicDoer interface {
		Do(*http.Request) (*http.Response, error)
	}
	trustedDoer interface {
		Do(*http.Request) (*http.Response, error)
	}
	validateURL   func(string) (*url.URL, error)
	rssHubOrigin  *url.URL
	rssHubBaseErr error
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
	origin, baseErr := parseRSSHubBaseURL(rssHubBaseURL)
	var trustedDoer interface {
		Do(*http.Request) (*http.Response, error)
	}
	if origin != nil {
		trustedDoer = newTrustedProviderHTTPClient(origin, 20*time.Second)
	}
	return ProviderClient{
		publicDoer:    httpx.NewClient(20 * time.Second),
		trustedDoer:   trustedDoer,
		validateURL:   httpx.ValidateURL,
		rssHubOrigin:  origin,
		rssHubBaseErr: baseErr,
		maxBodyBytes:  defaultProviderBodyBytes,
		userAgent:     providerUserAgent,
	}
}

func (c ProviderClient) Fetch(ctx context.Context, endpoint, etag, lastModified string) (ProviderFetchResult, error) {
	parsed, trusted, err := c.resolveEndpoint(endpoint)
	if err != nil {
		return ProviderFetchResult{}, err
	}
	if parsed.User != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ProviderFetchResult{}, fmt.Errorf("invalid provider endpoint")
	}
	var client interface {
		Do(*http.Request) (*http.Response, error)
	}
	if trusted {
		client = c.trustedDoer
		if client == nil {
			client = newTrustedProviderHTTPClient(c.rssHubOrigin, 20*time.Second)
		}
	} else {
		validate := c.validateURL
		if validate == nil {
			validate = httpx.ValidateURL
		}
		if _, err := validate(parsed.String()); err != nil {
			return ProviderFetchResult{}, fmt.Errorf("validate provider endpoint: %w", err)
		}
		client = c.publicDoer
		if client == nil {
			client = httpx.NewClient(20 * time.Second)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
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

func (c ProviderClient) resolveEndpoint(endpoint string) (*url.URL, bool, error) {
	raw := strings.TrimSpace(endpoint)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false, fmt.Errorf("parse provider endpoint: %w", err)
	}
	if u.IsAbs() {
		return u, false, nil
	}
	if u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return nil, false, fmt.Errorf("provider endpoint must be an absolute path or absolute URL")
	}
	if !isSafeRSSHubPath(raw, u) {
		return nil, false, fmt.Errorf("invalid RSSHub provider path")
	}
	if c.rssHubBaseErr != nil {
		return nil, false, c.rssHubBaseErr
	}
	if c.rssHubOrigin == nil {
		return nil, false, fmt.Errorf("relative provider endpoint requires RSSHub base URL")
	}
	resolved := c.rssHubOrigin.ResolveReference(u)
	if resolved.User != nil || !sameProviderOrigin(c.rssHubOrigin, resolved) {
		return nil, false, fmt.Errorf("RSSHub provider endpoint escaped configured origin")
	}
	return resolved, true, nil
}

func parseRSSHubBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.Contains(raw, "\\") {
		return nil, fmt.Errorf("invalid RSSHub base URL")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid RSSHub base URL")
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid RSSHub base URL")
	}
	if port := u.Port(); port != "" {
		parsed, err := strconv.ParseUint(port, 10, 16)
		if err != nil || parsed == 0 {
			return nil, fmt.Errorf("invalid RSSHub base URL")
		}
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}

func isSafeRSSHubPath(raw string, u *url.URL) bool {
	if u == nil || u.Fragment != "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
		return false
	}
	rawPath := raw
	if query := strings.IndexByte(rawPath, '?'); query >= 0 {
		rawPath = rawPath[:query]
	}
	lowerPath := strings.ToLower(rawPath)
	return !strings.Contains(lowerPath, "%2f") && !strings.Contains(lowerPath, "%5c")
}

func sameProviderOrigin(want, got *url.URL) bool {
	return want != nil && got != nil && strings.EqualFold(want.Scheme, got.Scheme) && strings.EqualFold(want.Host, got.Host)
}

func newTrustedProviderHTTPClient(origin *url.URL, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if request.URL.User != nil || !sameProviderOrigin(origin, request.URL) {
			return errors.New("RSSHub redirect escaped configured origin")
		}
		return nil
	}
	return client
}

// NormalizeCandidates rejects unsafe URLs and coalesces candidates by their
// canonical feed URL. The lexically smallest stable key wins public metadata.
func NormalizeCandidates(input []Candidate) []Candidate {
	collector := newCandidateCollector(maxProviderCandidates, func(candidate Candidate) string { return candidate.FeedURL })
	for _, raw := range input {
		raw, ok := normalizeCandidate(raw)
		if !ok {
			continue
		}
		collector.add(raw)
	}
	return collector.candidates()
}

func checkProviderBody(body []byte) error {
	if len(body) > defaultProviderBodyBytes {
		return fmt.Errorf("provider body exceeds %d bytes", defaultProviderBodyBytes)
	}
	return nil
}

// ValidateCandidate is the repository's defensive boundary for values that
// should already have been normalized by provider adapters.
func ValidateCandidate(candidate Candidate) error {
	if strings.ContainsRune(candidate.ExternalKey, 0) || strings.ContainsRune(candidate.FeedURL, 0) || strings.ContainsRune(candidate.SiteURL, 0) || strings.ContainsRune(candidate.Title, 0) || strings.ContainsRune(candidate.Topic, 0) {
		return errors.New("candidate contains NUL")
	}
	if candidate.ExternalKey == "" || !utf8.ValidString(candidate.ExternalKey) || len(candidate.ExternalKey) > maxCandidateKeyBytes {
		return errors.New("invalid candidate external key")
	}
	if candidate.FeedURL == "" || !utf8.ValidString(candidate.FeedURL) || len(candidate.FeedURL) > maxCandidateFeedURLBytes {
		return errors.New("invalid candidate feed URL")
	}
	if !utf8.ValidString(candidate.SiteURL) || len(candidate.SiteURL) > maxCandidateSiteURLBytes || !utf8.ValidString(candidate.Title) || len(candidate.Title) > maxCandidateTitleBytes || !utf8.ValidString(candidate.Topic) || len(candidate.Topic) > maxCandidateTopicBytes || len(candidate.Tags) > maxCandidateTagCount {
		return errors.New("invalid candidate public metadata")
	}
	for _, tag := range candidate.Tags {
		if strings.ContainsRune(tag, 0) {
			return errors.New("candidate tag contains NUL")
		}
		if !utf8.ValidString(tag) || len(tag) > maxCandidateTopicBytes {
			return errors.New("invalid candidate tag")
		}
	}
	if candidate.OccurrenceCount < 1 || candidate.OccurrenceCount > maxCandidateOccurrence {
		return errors.New("invalid candidate occurrence count")
	}
	if normalized, ok := normalizePublicURL(candidate.FeedURL); !ok || normalized != candidate.FeedURL {
		return errors.New("candidate feed URL is not canonical public URL")
	}
	if candidate.SiteURL != "" {
		if normalized, ok := normalizePublicURL(candidate.SiteURL); !ok || normalized != candidate.SiteURL {
			return errors.New("candidate site URL is not canonical public URL")
		}
	}
	return nil
}

func normalizeCandidate(candidate Candidate) (Candidate, bool) {
	if strings.ContainsRune(candidate.FeedURL, 0) {
		return Candidate{}, false
	}
	if strings.ContainsRune(candidate.SiteURL, 0) {
		candidate.SiteURL = ""
	}
	if !utf8.ValidString(candidate.FeedURL) || len(candidate.FeedURL) > maxCandidateFeedURLBytes {
		return Candidate{}, false
	}
	feedURL, ok := normalizePublicURL(candidate.FeedURL)
	if !ok || len(feedURL) > maxCandidateFeedURLBytes {
		return Candidate{}, false
	}
	candidate.FeedURL = feedURL
	if !utf8.ValidString(candidate.SiteURL) || len(candidate.SiteURL) > maxCandidateSiteURLBytes {
		candidate.SiteURL = ""
	} else if siteURL, ok := normalizePublicURL(candidate.SiteURL); ok && len(siteURL) <= maxCandidateSiteURLBytes {
		candidate.SiteURL = siteURL
	} else {
		candidate.SiteURL = ""
	}
	candidate.Title = clipUTF8(strings.ReplaceAll(candidate.Title, "\x00", ""), maxCandidateTitleBytes)
	candidate.Topic = clipUTF8(strings.ReplaceAll(candidate.Topic, "\x00", ""), maxCandidateTopicBytes)
	if !utf8.ValidString(candidate.ExternalKey) || len(candidate.ExternalKey) > maxCandidateKeyBytes || candidate.ExternalKey == "" {
		sum := sha256.Sum256([]byte(candidate.ExternalKey + "\x00" + feedURL))
		candidate.ExternalKey = "sha256:" + hex.EncodeToString(sum[:])
	}
	tags := make([]string, 0, maxCandidateTagCount)
	seen := map[string]struct{}{}
	for _, tag := range candidate.Tags {
		if strings.ContainsRune(tag, 0) {
			continue
		}
		tag = clipUTF8(tag, maxCandidateTopicBytes)
		if tag != "" {
			if _, ok := seen[tag]; !ok {
				seen[tag] = struct{}{}
				tags = append(tags, tag)
				if len(tags) == maxCandidateTagCount {
					break
				}
			}
		}
	}
	candidate.Tags = uniqueStrings(tags)
	if candidate.OccurrenceCount < 1 {
		candidate.OccurrenceCount = 1
	}
	if candidate.OccurrenceCount > maxCandidateOccurrence {
		candidate.OccurrenceCount = maxCandidateOccurrence
	}
	return candidate, true
}

func clipUTF8(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
func ClipCandidateTitle(value string) string {
	return clipUTF8(strings.ReplaceAll(value, "\x00", ""), maxCandidateTitleBytes)
}
func safeOccurrenceAdd(left, right int) int {
	if left >= maxCandidateOccurrence-right {
		return maxCandidateOccurrence
	}
	return left + right
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
