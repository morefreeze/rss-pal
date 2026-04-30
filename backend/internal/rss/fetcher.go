package rss

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

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
	req.Header.Set("User-Agent", userAgent)

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
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.9")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

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

	// Try HTML scraping first (no extra requests)
	htmlResult := f.scrapeHTMLArticles(doc, rawURL)
	if len(htmlResult.Items) >= 3 {
		return htmlResult, nil
	}

	// HTML scraping found too few articles; probe common RSS paths concurrently
	type rssHit struct {
		result *PreviewResult
	}
	hitCh := make(chan rssHit, 1)
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	candidates := []string{"/feed", "/rss", "/rss.xml", "/atom.xml", "/feed.xml", "/index.xml"}
	baseURL2 := strings.TrimRight(rawURL, "/")
	probeBases := []string{baseURL2}

	// Also probe from root domain when URL has a sub-path
	if parsed, err := url.Parse(rawURL); err == nil {
		rootBase := parsed.Scheme + "://" + parsed.Host
		if rootBase != baseURL2 {
			probeBases = append(probeBases, rootBase)
		}
	}

	for _, base := range probeBases {
		for _, suffix := range candidates {
			go func(candidate string) {
				res, err := f.Fetch(probeCtx, candidate, "", "")
				if err == nil && res != nil && len(res.Feed.Items) > 0 {
					r := rssToPreview(res.Feed, candidate)
					r.ActualURL = candidate
					select {
					case hitCh <- rssHit{r}:
					default:
					}
				}
			}(base + suffix)
		}
	}

	select {
	case hit := <-hitCh:
		return hit.result, nil
	case <-probeCtx.Done():
	}

	return htmlResult, nil
}

// FetchHTML fetches an HTML page and extracts article links as a synthetic feed
func (f *Fetcher) FetchHTML(ctx context.Context, feedURL string) (*gofeed.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

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
		gi := &gofeed.Item{Title: item.Title, Link: item.URL}
		if item.PublishedAt != nil {
			dateFormats := []string{
				time.RFC3339,
				"2006-01-02T15:04:05Z07:00",
				"2006-01-02T15:04:05",
				"2006-01-02 15:04:05",
				"2006-01-02 15:04",
				"2006-01-02",
				"2006/01/02",
				"Jan 02, 2006",
				"Jan. 02, 2006",
				"January 02, 2006",
				"January 2, 2006",
				"02 Jan 2006",
				"2006年01月02日",
				"2006年1月2日",
				"2006年01月02日 15:04",
			}
			for _, format := range dateFormats {
				if t, err := time.Parse(format, *item.PublishedAt); err == nil {
					gi.PublishedParsed = &t
					break
				}
			}
		}
		syntheticFeed.Items = append(syntheticFeed.Items, gi)
	}
	return syntheticFeed, nil
}

// extractJSONLDArticles tries to find article links from JSON-LD structured data
func extractJSONLDArticles(doc *goquery.Document, baseURL string) []PreviewItem {
	var items []PreviewItem
	seen := map[string]bool{}
	base, _ := url.Parse(baseURL)

	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		if len(items) >= 10 {
			return
		}
		var raw interface{}
		if err := json.Unmarshal([]byte(s.Text()), &raw); err != nil {
			return
		}
		// Walk the JSON looking for @type:Article/BlogPosting/NewsArticle and url/name fields
		var walk func(v interface{})
		walk = func(v interface{}) {
			if len(items) >= 10 {
				return
			}
			switch node := v.(type) {
			case map[string]interface{}:
				t, _ := node["@type"].(string)
				if t == "Article" || t == "BlogPosting" || t == "NewsArticle" || t == "ListItem" {
					rawURL, _ := node["url"].(string)
					name, _ := node["name"].(string)
					if name == "" {
						name, _ = node["headline"].(string)
					}
					if rawURL != "" && name != "" && len(name) >= 8 {
						absURL := resolveURL(baseURL, rawURL)
						parsed, err := url.Parse(absURL)
						if err == nil && !seen[absURL] && (base == nil || parsed.Hostname() == base.Hostname()) && !isBoringURL(parsed) {
							seen[absURL] = true
							items = append(items, PreviewItem{Title: name, URL: absURL})
						}
					}
				}
				for _, child := range node {
					walk(child)
				}
			case []interface{}:
				for _, child := range node {
					walk(child)
				}
			}
		}
		walk(raw)
	})
	return items
}

func (f *Fetcher) scrapeHTMLArticles(doc *goquery.Document, baseURL string) *PreviewResult {
	pageTitle := strings.TrimSpace(doc.Find("title").First().Text())
	// Prefer og:site_name over page title for list pages (page title often contains article title)
	if siteName := strings.TrimSpace(doc.Find(`meta[property="og:site_name"]`).AttrOr("content", "")); siteName != "" {
		pageTitle = siteName
	}

	// Try JSON-LD structured data first (most reliable when available)
	if jsonItems := extractJSONLDArticles(doc, baseURL); len(jsonItems) >= 3 {
		return &PreviewResult{
			FeedTitle: pageTitle,
			FeedType:  "html",
			ActualURL: baseURL,
			Items:     jsonItems,
		}
	}

	// Remove chrome elements to avoid nav/footer links
	doc.Find("nav, header, footer, aside, .sidebar, .nav, .menu, .navigation, .header, .footer, .widget, .ad, .advertisement, [role=navigation], [role=banner], [role=complementary]").Remove()

	base, _ := url.Parse(baseURL)
	seen := map[string]bool{}
	var items []PreviewItem

	// Try article-specific selectors first, then fall back to all links
	selectors := []string{
		"article a[href]", "main a[href]",
		".post-list a[href]", ".posts a[href]", ".blog-list a[href]",
		".entries a[href]", ".content a[href]",
		".news-list a[href]", ".news-item a[href]", ".news-feed a[href]",
		".story-list a[href]", ".story-item a[href]",
		".article-list a[href]",
		".card a[href]", ".cards a[href]",
		"[class*=post] a[href]", "[class*=article] a[href]", "[class*=entry] a[href]",
		"[class*=news] a[href]", "[class*=story] a[href]", "[class*=item] a[href]",
		"h1 a[href]", "h2 a[href]", "h3 a[href]",
		"ul li a[href]", "ol li a[href]",
		"a[href]",
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

			// Extract date from <time> elements inside the link before stripping them from title
			var publishedAt *string
			s.Find("time").Each(func(_ int, t *goquery.Selection) {
				if publishedAt != nil {
					return
				}
				dt := strings.TrimSpace(t.AttrOr("datetime", ""))
				if dt == "" {
					dt = strings.TrimSpace(t.Text())
				}
				if dt != "" {
					publishedAt = &dt
				}
			})

			// Try to find the best title: link text (strip nested date elements), parent heading, or img alt
			titleSel := s.Clone()
			titleSel.Find("time").Remove()
			title := strings.TrimSpace(titleSel.Text())
			if len(title) < 6 {
				heading := s.Closest("h1, h2, h3, h4")
				if heading.Length() > 0 {
					title = strings.TrimSpace(heading.Text())
				}
			}
			if len(title) < 6 {
				// Try parent container heading
				parent := s.Parent()
				for i := 0; i < 3 && parent.Length() > 0; i++ {
					heading := parent.Find("h1, h2, h3, h4").First()
					if heading.Length() > 0 {
						t := strings.TrimSpace(heading.Text())
						if len(t) >= 6 {
							title = t
							break
						}
					}
					parent = parent.Parent()
				}
			}
			if len(title) < 6 {
				imgAlt := s.Find("img").First().AttrOr("alt", "")
				if len(imgAlt) >= 6 {
					title = imgAlt
				}
			}
			if len(title) < 6 {
				return
			}
			// Trim overly long titles (likely scraped nav text)
			if len([]rune(title)) > 200 {
				return
			}

			// If no date found inside the link, check the nearest container for <time> elements
			if publishedAt == nil {
				container := s.Closest("article, li, .post, .entry, [class*=item]")
				if container.Length() == 0 {
					container = s.Parent()
				}
				container.Find("time").Each(func(_ int, t *goquery.Selection) {
					if publishedAt != nil {
						return
					}
					dt := strings.TrimSpace(t.AttrOr("datetime", ""))
					if dt == "" {
						dt = strings.TrimSpace(t.Text())
					}
					if dt != "" {
						publishedAt = &dt
					}
				})
			}

			seen[absURL] = true
			items = append(items, PreviewItem{Title: title, URL: absURL, PublishedAt: publishedAt})
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
		"/search", "/404", "/archives", "/author", "/authors",
		"/subscribe", "/newsletter", "/rss.xml", "/atom.xml",
		"/help", "/faq", "/support", "/advertise",
	}
	for _, b := range boring {
		if p == b || p == b+"/" {
			return true
		}
	}
	// Skip tag/category/author/page sub-paths (e.g. /tag/golang, /page/2)
	boringPrefixes := []string{"/tag/", "/tags/", "/category/", "/categories/", "/author/", "/authors/", "/page/", "/search/"}
	for _, prefix := range boringPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	// Skip common file extensions that aren't articles
	boringExt := []string{".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".pdf", ".zip", ".mp3", ".mp4"}
	for _, ext := range boringExt {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	// Must have at least one meaningful segment: 4+ chars, 3-char alphanumeric slug,
	// numeric ID, or hyphenated slug
	segments := strings.Split(strings.Trim(p, "/"), "/")
	for _, seg := range segments {
		if len(seg) >= 4 {
			return false
		}
		// Allow short numeric IDs (e.g. /p/123) and short hyphenated slugs
		if len(seg) >= 3 && (isAllDigits(seg) || strings.Contains(seg, "-")) {
			return false
		}
	}
	return true
}

func isHTMLContentType(ct string) bool {
	return strings.Contains(ct, "html")
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
