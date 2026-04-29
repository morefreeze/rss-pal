package rss

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type Fetcher struct {
	parser *gofeed.Parser
	client *http.Client
}

func NewFetcher() *Fetcher {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	return &Fetcher{
		parser: gofeed.NewParser(),
		client: client,
	}
}

type FetchResult struct {
	Feed         *gofeed.Feed
	ETag         string
	LastModified string
}

// PreviewItem is a lightweight article preview (no DB write yet)
type PreviewItem struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	PublishedAt *string `json:"published_at,omitempty"`
}

// PreviewResult is returned by Preview() before the feed is saved
type PreviewResult struct {
	FeedTitle string        `json:"feed_title"`
	FeedType  string        `json:"feed_type"`  // "rss" or "html"
	ActualURL string        `json:"actual_url"` // may differ from input (RSS found in HTML)
	Items     []PreviewItem `json:"items"`
}

func (f *Fetcher) Fetch(ctx context.Context, feedURL string, etag, lastModified string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSPal/1.0)")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	feed, err := f.parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}

	return &FetchResult{
		Feed:         feed,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

// Preview fetches a URL and returns up to 10 articles without saving anything.
// Tries RSS/Atom first, then auto-discovers RSS link in HTML, then falls back to scraping.
func (f *Fetcher) Preview(ctx context.Context, rawURL string) (*PreviewResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSPal/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.9")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	body := buf.Bytes()
	ct := resp.Header.Get("Content-Type")

	// Try RSS/Atom parse if content-type suggests XML, or as first attempt for unknown types
	if !isHTMLContentType(ct) {
		feed, err := f.parser.ParseString(string(body))
		if err == nil && len(feed.Items) > 0 {
			return rssToPreview(feed, rawURL), nil
		}
	}

	// Parse as HTML to look for RSS autodiscovery link
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to parse page: %w", err)
	}

	var discoveredFeedURL string
	doc.Find(`link[rel="alternate"]`).Each(func(i int, s *goquery.Selection) {
		linkType, _ := s.Attr("type")
		href, _ := s.Attr("href")
		if discoveredFeedURL == "" && href != "" &&
			(strings.Contains(linkType, "rss") || strings.Contains(linkType, "atom")) {
			discoveredFeedURL = resolveURL(rawURL, href)
		}
	})

	if discoveredFeedURL != "" {
		rssResult, err := f.Fetch(ctx, discoveredFeedURL, "", "")
		if err == nil && rssResult != nil && len(rssResult.Feed.Items) > 0 {
			result := rssToPreview(rssResult.Feed, discoveredFeedURL)
			result.ActualURL = discoveredFeedURL
			return result, nil
		}
	}

	// Fall back to HTML link scraping
	return f.scrapeHTMLArticles(doc, rawURL), nil
}

// FetchHTML fetches an HTML page and extracts article links as a synthetic feed
func (f *Fetcher) FetchHTML(ctx context.Context, feedURL string) (*gofeed.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSPal/1.0)")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	preview := f.scrapeHTMLArticles(doc, feedURL)

	syntheticFeed := &gofeed.Feed{Title: preview.FeedTitle}
	for _, item := range preview.Items {
		syntheticFeed.Items = append(syntheticFeed.Items, &gofeed.Item{
			Title: item.Title,
			Link:  item.URL,
		})
	}
	return syntheticFeed, nil
}

func (f *Fetcher) scrapeHTMLArticles(doc *goquery.Document, baseURL string) *PreviewResult {
	pageTitle := strings.TrimSpace(doc.Find("title").First().Text())

	// Remove chrome elements to avoid nav/footer links
	doc.Find("nav, header, footer, aside, .sidebar, .nav, .menu, .navigation, .header, .footer, .widget").Remove()

	base, _ := url.Parse(baseURL)
	seen := map[string]bool{}
	var items []PreviewItem

	// Try article-specific selectors first, then fall back to all links
	selectors := []string{
		"article a", "main a", ".post-list a", ".posts a", ".blog-list a",
		".entries a", ".content a", "h1 a", "h2 a", "h3 a",
		"a",
	}

	for _, sel := range selectors {
		if len(items) >= 10 {
			break
		}
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			if len(items) >= 10 {
				return
			}
			href, exists := s.Attr("href")
			if !exists {
				return
			}

			absURL := resolveURL(baseURL, href)
			if absURL == "" || seen[absURL] {
				return
			}

			parsed, err := url.Parse(absURL)
			if err != nil || (base != nil && parsed.Hostname() != base.Hostname()) {
				return
			}

			if isBoringURL(parsed) {
				return
			}

			title := strings.TrimSpace(s.Text())
			if len(title) < 10 {
				heading := s.Closest("h1, h2, h3, h4")
				if heading.Length() > 0 {
					title = strings.TrimSpace(heading.Text())
				}
			}
			if len(title) < 10 {
				return
			}

			seen[absURL] = true
			items = append(items, PreviewItem{Title: title, URL: absURL})
		})
		if len(items) >= 5 {
			break
		}
	}

	return &PreviewResult{
		FeedTitle: pageTitle,
		FeedType:  "html",
		ActualURL: baseURL,
		Items:     items,
	}
}

func rssToPreview(feed *gofeed.Feed, feedURL string) *PreviewResult {
	var items []PreviewItem
	for i, item := range feed.Items {
		if i >= 10 {
			break
		}
		pi := PreviewItem{Title: item.Title, URL: item.Link}
		t := publishedTimeStr(item.PublishedParsed, item.UpdatedParsed)
		if t != "" {
			pi.PublishedAt = &t
		}
		items = append(items, pi)
	}
	return &PreviewResult{
		FeedTitle: feed.Title,
		FeedType:  "rss",
		ActualURL: feedURL,
		Items:     items,
	}
}

func publishedTimeStr(published, updated *time.Time) string {
	t := published
	if t == nil {
		t = updated
	}
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func resolveURL(base, href string) string {
	if href == "" || strings.HasPrefix(href, "#") ||
		strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return b.ResolveReference(ref).String()
}

func isBoringURL(u *url.URL) bool {
	p := strings.ToLower(u.Path)
	boring := []string{
		"/", "/about", "/contact", "/feed", "/rss", "/sitemap",
		"/login", "/signup", "/register", "/privacy", "/terms",
		"/tag", "/tags", "/category", "/categories", "/page",
		"/search", "/404", "/archives",
	}
	for _, b := range boring {
		if p == b || p == b+"/" {
			return true
		}
	}
	// Must have at least one segment longer than 5 chars (filters out /p/1, /c/go, etc.)
	segments := strings.Split(strings.Trim(p, "/"), "/")
	for _, seg := range segments {
		if len(seg) > 5 {
			return false
		}
	}
	return true
}

func isHTMLContentType(ct string) bool {
	return strings.Contains(ct, "html")
}
