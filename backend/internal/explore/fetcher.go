package explore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

const (
	defaultSourceBodyBytes      = int64(4 << 20)
	maxSourceBodyBytes          = int64(4 << 20)
	maxDiscoveryCandidates      = 4
	maxExploreArticles          = 50
	maxExploreTitleBytes        = 500
	maxExploreThumbnailURLBytes = 2048
	maxArticleClockSkew         = 24 * time.Hour

	// SourceFetchRequestTimeout bounds each independent FetchBounded call.
	// SourceFetchMaxRequests includes the initial URL plus every supported
	// rel=alternate candidate, and is shared with the queue lease budget.
	SourceFetchRequestTimeout = 20 * time.Second
	SourceFetchMaxRequests    = 1 + maxDiscoveryCandidates
)

// ErrInsufficientSourceConfidence lets the queue processor distinguish a
// terminal content problem from registry evidence that may still be racing.
var (
	ErrInsufficientSourceConfidence = errors.New("source has insufficient public observation evidence")
	ErrInactiveSource               = errors.New("source has no recent article output")
)

// ObservationEvidence contains only public registry evidence. It deliberately
// has no user or private-article provenance fields.
type ObservationEvidence struct {
	ProviderID            int
	ProviderKind          string
	Enabled               bool
	ProviderLastSuccessAt *time.Time
	LastSeenAt            time.Time
	OccurrenceCount       int
}

// HasSourceConfidence applies the public cold-start evidence thresholds. A
// direct profile candidate represents an ephemeral rel=alternate discovery;
// it skips only this provenance gate, never the safety/content/freshness gates.
func HasSourceConfidence(now time.Time, evidence []ObservationEvidence, directProfile bool) bool {
	if directProfile {
		return true
	}
	providerIDs := make(map[int]struct{}, 2)
	for _, item := range evidence {
		if !usableEvidence(now, item) {
			continue
		}
		switch item.ProviderKind {
		case "opml", "directory", "github_awesome":
			return true
		case "reddit_stream", "related_site":
			if item.OccurrenceCount >= 2 {
				return true
			}
		}
		if item.ProviderID > 0 {
			providerIDs[item.ProviderID] = struct{}{}
			if len(providerIDs) >= 2 {
				return true
			}
		}
	}
	return false
}

func usableEvidence(now time.Time, item ObservationEvidence) bool {
	if !item.Enabled || item.ProviderID <= 0 || item.ProviderLastSuccessAt == nil || item.LastSeenAt.IsZero() {
		return false
	}
	return !item.ProviderLastSuccessAt.Before(now.Add(-7*24*time.Hour)) &&
		!item.LastSeenAt.Before(now.Add(-30*24*time.Hour))
}

type SourceFetchErrorKind string

const (
	SourceFetchRetryable SourceFetchErrorKind = "retryable"
	SourceFetchTerminal  SourceFetchErrorKind = "terminal"
)

type sourceFetchError struct {
	kind SourceFetchErrorKind
	err  error
}

func (e *sourceFetchError) Error() string { return e.err.Error() }
func (e *sourceFetchError) Unwrap() error { return e.err }

// ClassifySourceFetchError is the queue-independent outcome classifier used
// by task handlers. A nil error has no classification.
func ClassifySourceFetchError(err error) SourceFetchErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrInactiveSource) {
		return SourceFetchRetryable
	}
	var classified *sourceFetchError
	if errors.As(err, &classified) {
		return classified.kind
	}
	if errors.Is(err, httpx.ErrResponseTooLarge) {
		return SourceFetchTerminal
	}
	return SourceFetchRetryable
}

func sourceError(kind SourceFetchErrorKind, format string, args ...any) error {
	return &sourceFetchError{kind: kind, err: fmt.Errorf(format, args...)}
}

type SourceFetchMode string

const (
	SourceFetchValidate SourceFetchMode = "validate"
	SourceFetchRefresh  SourceFetchMode = "refresh"
)

type SourceFetchRequest struct {
	URL           string
	Mode          SourceFetchMode
	ETag          string
	LastModified  string
	Evidence      []ObservationEvidence
	DirectProfile bool
}

type SourceFetchResult struct {
	FeedURL      string
	Articles     []model.ExploreArticle
	ETag         string
	LastModified string
	NotModified  bool
}

// SourceFetcher fetches and parses candidate sources without persistence. A
// zero-value fetcher is production-safe; tests may inject an HTTP client in
// this package without exposing a public SSRF bypass.
type SourceFetcher struct {
	client       *http.Client
	now          func() time.Time
	maxBodyBytes int64
}

func NewSourceFetcher() *SourceFetcher {
	return &SourceFetcher{client: httpx.NewClient(SourceFetchRequestTimeout), now: time.Now, maxBodyBytes: defaultSourceBodyBytes}
}

func (f *SourceFetcher) Fetch(ctx context.Context, request SourceFetchRequest) (SourceFetchResult, error) {
	now := time.Now()
	if f != nil && f.now != nil {
		now = f.now()
	}
	requireConfidence := request.Mode != SourceFetchRefresh
	if requireConfidence && !HasSourceConfidence(now, request.Evidence, request.DirectProfile) {
		return SourceFetchResult{}, sourceError(SourceFetchTerminal, "%w", ErrInsufficientSourceConfidence)
	}
	minimumArticles := 2
	etag, lastModified := request.ETag, request.LastModified
	if request.Mode == SourceFetchRefresh {
		minimumArticles = 1
		// Refresh is also the liveness check. Conditional 304 responses cannot
		// prove that the source still emits recent articles.
		etag, lastModified = "", ""
	}
	result, err := f.fetch(ctx, request.URL, etag, lastModified, now, true, minimumArticles)
	if err == nil && request.Mode == SourceFetchRefresh && result.NotModified {
		return SourceFetchResult{}, sourceError(SourceFetchTerminal, "%w: refresh returned no body", ErrInactiveSource)
	}
	return result, err
}

func (f *SourceFetcher) fetch(ctx context.Context, rawURL, etag, lastModified string, now time.Time, allowDiscovery bool, minimumArticles int) (SourceFetchResult, error) {
	headers := make(http.Header)
	if etag != "" {
		headers.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		headers.Set("If-Modified-Since", lastModified)
	}
	client := (*http.Client)(nil)
	if f != nil {
		client = f.client
	}
	if client == nil {
		client = httpx.NewClient(SourceFetchRequestTimeout)
	}
	limit := defaultSourceBodyBytes
	if f != nil && f.maxBodyBytes > 0 {
		limit = f.maxBodyBytes
	}
	if limit > maxSourceBodyBytes {
		limit = maxSourceBodyBytes
	}
	response, err := httpx.FetchBounded(ctx, client, rawURL, headers, limit)
	if err != nil {
		if response != nil {
			if response.StatusCode == http.StatusNotModified {
				finalURL := response.FinalURL
				if finalURL == "" {
					finalURL = rawURL
				}
				canonicalURL, valid := normalizePublicURL(finalURL)
				if !valid {
					return SourceFetchResult{}, sourceError(SourceFetchTerminal, "unsafe final source URL")
				}
				return SourceFetchResult{
					FeedURL:      canonicalURL,
					ETag:         response.Header.Get("ETag"),
					LastModified: response.Header.Get("Last-Modified"),
					NotModified:  true,
				}, nil
			}
			kind := classifyHTTPStatus(response.StatusCode)
			return SourceFetchResult{}, sourceError(kind, "fetch source: %w", err)
		}
		kind := SourceFetchRetryable
		if errors.Is(err, httpx.ErrResponseTooLarge) || isUnsafeFetchError(err) {
			kind = SourceFetchTerminal
		}
		return SourceFetchResult{}, sourceError(kind, "fetch source: %w", err)
	}
	result := SourceFetchResult{
		FeedURL:      response.FinalURL,
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}
	if result.FeedURL == "" {
		result.FeedURL = rawURL
	}
	canonicalURL, valid := normalizePublicURL(result.FeedURL)
	if !valid {
		return SourceFetchResult{}, sourceError(SourceFetchTerminal, "unsafe final source URL")
	}
	result.FeedURL = canonicalURL
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	articles, recognizedFeed, parseErr := parseExploreFeed(response.Body, result.FeedURL, now, minimumArticles)
	if recognizedFeed {
		if parseErr != nil {
			return SourceFetchResult{}, sourceError(SourceFetchTerminal, "parse source: %w", parseErr)
		}
		result.Articles = articles
		return result, nil
	}
	if allowDiscovery && isHTMLDocument(response.Header.Get("Content-Type"), response.Body) {
		candidates, err := discoverFeedURLs(response.Body, result.FeedURL)
		if err != nil {
			return SourceFetchResult{}, sourceError(SourceFetchTerminal, "discover feed: %w", err)
		}
		if len(candidates) == 0 {
			return SourceFetchResult{}, sourceError(SourceFetchTerminal, "HTML page has no supported feed alternate")
		}
		var retryable error
		for _, candidate := range candidates {
			candidateResult, candidateErr := f.fetch(ctx, candidate, "", "", now, false, minimumArticles)
			if candidateErr == nil {
				return candidateResult, nil
			}
			if ClassifySourceFetchError(candidateErr) == SourceFetchRetryable {
				retryable = candidateErr
			}
		}
		if retryable != nil {
			return SourceFetchResult{}, retryable
		}
		return SourceFetchResult{}, sourceError(SourceFetchTerminal, "no discovered alternate is a valid feed")
	}
	if parseErr == nil {
		parseErr = errors.New("document is not a feed")
	}
	return SourceFetchResult{}, sourceError(SourceFetchTerminal, "parse source: %w", parseErr)
}

func classifyHTTPStatus(status int) SourceFetchErrorKind {
	if status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500 {
		return SourceFetchRetryable
	}
	return SourceFetchTerminal
}

func isUnsafeFetchError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"blocked address", "blocked port", "invalid port", "credentials are not allowed", "unsupported scheme", "missing host", "empty url", "parse:", "redirect rejected", "response exceeds configured limit"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func isHTMLDocument(contentType string, body []byte) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return true
	}
	prefix := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 256)])))
	return strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<html")
}

func discoverFeedURLs(body []byte, finalURL string) ([]string, error) {
	base, err := url.Parse(finalURL)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, maxDiscoveryCandidates)
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			if tokenizer.Err() != nil && !errors.Is(tokenizer.Err(), io.EOF) {
				return nil, tokenizer.Err()
			}
			out := make([]string, 0, len(selected))
			for candidate := range selected {
				out = append(out, candidate)
			}
			sort.Strings(out)
			return out, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "link") {
				continue
			}
			var rel, feedType, href string
			for _, attribute := range token.Attr {
				switch strings.ToLower(attribute.Key) {
				case "rel":
					rel = attribute.Val
				case "type":
					feedType = attribute.Val
				case "href":
					href = attribute.Val
				}
			}
			if !containsToken(rel, "alternate") || !supportedFeedMIME(feedType) || strings.TrimSpace(href) == "" {
				continue
			}
			reference, parseErr := url.Parse(strings.TrimSpace(href))
			if parseErr != nil || reference.User != nil {
				continue
			}
			candidate, valid := normalizePublicURL(base.ResolveReference(reference).String())
			if !valid {
				continue
			}
			insertLexicographicTop(selected, candidate, maxDiscoveryCandidates)
		}
	}
}

func containsToken(value, wanted string) bool {
	for _, token := range strings.Fields(strings.ToLower(value)) {
		if token == wanted {
			return true
		}
	}
	return false
}

func supportedFeedMIME(value string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	return mediaType == "application/rss+xml" || mediaType == "application/atom+xml"
}

func insertLexicographicTop(values map[string]struct{}, value string, limit int) {
	if _, exists := values[value]; exists {
		return
	}
	values[value] = struct{}{}
	if len(values) <= limit {
		return
	}
	var largest string
	for candidate := range values {
		if candidate > largest {
			largest = candidate
		}
	}
	delete(values, largest)
}

func parseExploreFeed(body []byte, feedURL string, fetchedAt time.Time, minimumArticles int) ([]model.ExploreArticle, bool, error) {
	parsed, err := gofeed.NewParser().ParseString(string(body))
	if err != nil {
		return nil, false, err
	}
	base, err := url.Parse(feedURL)
	if err != nil {
		return nil, true, err
	}
	byNormalized := make(map[string]model.ExploreArticle)
	for _, item := range parsed.Items {
		article, ok := exploreArticleFromItem(item, base, fetchedAt)
		if !ok {
			continue
		}
		current, exists := byNormalized[article.NormalizedURL]
		if !exists || articleBefore(article, current) {
			byNormalized[article.NormalizedURL] = article
		}
	}
	articles := make([]model.ExploreArticle, 0, len(byNormalized))
	for _, article := range byNormalized {
		articles = append(articles, article)
	}
	sort.Slice(articles, func(i, j int) bool { return articleBefore(articles[i], articles[j]) })
	if minimumArticles > 0 {
		if len(articles) < minimumArticles {
			if minimumArticles == 1 {
				return nil, true, fmt.Errorf("%w: feed has no parseable articles", ErrInactiveSource)
			}
			return nil, true, errors.New("feed must contain at least two parseable articles")
		}
		cutoff := fetchedAt.Add(-90 * 24 * time.Hour)
		futureLimit := fetchedAt.Add(maxArticleClockSkew)
		recent := false
		for _, article := range articles {
			if article.PublishedAt != nil && !article.PublishedAt.Before(cutoff) && !article.PublishedAt.After(futureLimit) {
				recent = true
				break
			}
		}
		if !recent {
			if minimumArticles == 1 {
				return nil, true, fmt.Errorf("%w: feed has no article published or updated in the last 90 days", ErrInactiveSource)
			}
			return nil, true, errors.New("feed has no article published or updated in the last 90 days")
		}
	}
	if len(articles) > maxExploreArticles {
		articles = articles[:maxExploreArticles]
	}
	return articles, true, nil
}

func exploreArticleFromItem(item *gofeed.Item, base *url.URL, fetchedAt time.Time) (model.ExploreArticle, bool) {
	if item == nil || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Link) == "" {
		return model.ExploreArticle{}, false
	}
	rawLink := strings.TrimSpace(item.Link)
	if strings.HasPrefix(rawLink, ":") {
		return model.ExploreArticle{}, false
	}
	reference, err := url.Parse(rawLink)
	if err != nil || reference.User != nil {
		return model.ExploreArticle{}, false
	}
	resolved := base.ResolveReference(reference)
	normalized, valid := normalizePublicURL(resolved.String())
	if !valid {
		return model.ExploreArticle{}, false
	}
	if normalized == "" {
		return model.ExploreArticle{}, false
	}
	article := model.ExploreArticle{
		URL:           resolved.String(),
		NormalizedURL: normalized,
		Title:         clipSourceUTF8(strings.TrimSpace(item.Title), maxExploreTitleBytes),
		FetchedAt:     fetchedAt,
	}
	var effective *time.Time
	if item.PublishedParsed != nil {
		published := *item.PublishedParsed
		effective = &published
	}
	if item.UpdatedParsed != nil && (effective == nil || item.UpdatedParsed.After(*effective)) {
		updated := *item.UpdatedParsed
		effective = &updated
	}
	if effective != nil {
		article.PublishedAt = effective
	}
	if content := strings.TrimSpace(item.Content); content != "" {
		article.Content = &content
	}
	if excerpt := strings.TrimSpace(item.Description); excerpt != "" {
		article.Excerpt = &excerpt
		if article.Content == nil {
			article.Content = &excerpt
		}
	}
	article.ThumbnailURL = exploreItemThumbnail(item, resolved)
	return article, true
}

func exploreItemThumbnail(item *gofeed.Item, articleURL *url.URL) *string {
	if item == nil || articleURL == nil {
		return nil
	}
	candidates := make([]string, 0, 4)
	if media := item.Extensions["media"]; media != nil {
		for _, name := range []string{"thumbnail", "content"} {
			for _, extension := range media[name] {
				if name != "content" || strings.HasPrefix(strings.ToLower(extension.Attrs["type"]), "image/") || extension.Attrs["medium"] == "image" {
					candidates = append(candidates, extension.Attrs["url"])
				}
			}
		}
	}
	if item.Image != nil {
		candidates = append(candidates, item.Image.URL)
	}
	for _, enclosure := range item.Enclosures {
		if enclosure != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(enclosure.Type)), "image/") {
			candidates = append(candidates, enclosure.URL)
		}
	}
	for _, body := range []string{item.Content, item.Description} {
		if image := firstExploreHTMLImage(body); image != "" {
			candidates = append(candidates, image)
		}
	}
	for _, raw := range candidates {
		reference, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || reference.User != nil {
			continue
		}
		resolved := articleURL.ResolveReference(reference)
		if len(resolved.String()) > maxExploreThumbnailURLBytes {
			continue
		}
		if normalized, ok := normalizePublicURL(resolved.String()); ok && len(normalized) <= maxExploreThumbnailURLBytes {
			return &normalized
		}
	}
	return nil
}

func firstExploreHTMLImage(body string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(body))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "img") {
				if src, ok := htmlAttribute(token, "src"); ok {
					return strings.TrimSpace(src)
				}
			}
		}
	}
}

func articleBefore(left, right model.ExploreArticle) bool {
	leftTime := left.FetchedAt
	rightTime := right.FetchedAt
	if left.PublishedAt != nil {
		leftTime = *left.PublishedAt
	}
	if right.PublishedAt != nil {
		rightTime = *right.PublishedAt
	}
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	return left.NormalizedURL < right.NormalizedURL
}

func clipSourceUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
