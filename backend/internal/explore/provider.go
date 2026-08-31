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
	if u.Scheme != "" || u.Host != "" {
		return "", fmt.Errorf("provider endpoint must be an absolute path or absolute URL")
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
		raw, ok := normalizeCandidate(raw)
		if !ok {
			continue
		}
		feedURL := raw.FeedURL
		if previous, exists := byURL[feedURL]; exists {
			previous.OccurrenceCount = safeOccurrenceAdd(previous.OccurrenceCount, raw.OccurrenceCount)
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
	if len(out) > maxProviderCandidates {
		out = out[:maxProviderCandidates]
	}
	return out
}

// ValidateCandidate is the repository's defensive boundary for values that
// should already have been normalized by provider adapters.
func ValidateCandidate(candidate Candidate) error {
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
		if !utf8.ValidString(tag) || len(tag) > maxCandidateTopicBytes {
			return errors.New("invalid candidate tag")
		}
	}
	if candidate.OccurrenceCount < 1 || candidate.OccurrenceCount > maxCandidateOccurrence {
		return errors.New("invalid candidate occurrence count")
	}
	return nil
}

func normalizeCandidate(candidate Candidate) (Candidate, bool) {
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
	candidate.Title = clipUTF8(candidate.Title, maxCandidateTitleBytes)
	candidate.Topic = clipUTF8(candidate.Topic, maxCandidateTopicBytes)
	if !utf8.ValidString(candidate.ExternalKey) || len(candidate.ExternalKey) > maxCandidateKeyBytes || candidate.ExternalKey == "" {
		sum := sha256.Sum256([]byte(candidate.ExternalKey + "\x00" + feedURL))
		candidate.ExternalKey = "sha256:" + hex.EncodeToString(sum[:])
	}
	tags := make([]string, 0, maxCandidateTagCount)
	seen := map[string]struct{}{}
	for _, tag := range candidate.Tags {
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
