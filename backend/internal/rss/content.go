package rss

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type ContentFetcher struct {
	client *http.Client
}

func NewContentFetcher() *ContentFetcher {
	return &ContentFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchContent fetches and extracts main content from a URL
func (f *ContentFetcher) FetchContent(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	// Set a realistic User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil // Don't fail, just return empty content
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	// Remove unwanted elements
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share").Remove()

	// Try to find main content
	var content string

	// Try common content selectors (ordered from most specific to least)
	selectors := []string{
		"article",
		"[role='main']",
		"main",
		".post-content",
		".article-content",
		".article-body",
		".entry-content",
		".story-body",
		".post-body",
		".field-item",
		".content",
		".post",
		"#content",
		"body",
	}

	for _, selector := range selectors {
		if doc.Find(selector).Length() > 0 {
			selection := doc.Find(selector).First()
			content = extractText(selection)
			if len(content) > 200 {
				break
			}
		}
	}

	if content == "" {
		// Fallback: get all paragraph text
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if len(text) > 30 {
				content += text + "\n\n"
			}
		})
	}

	// Clean up content
	content = cleanContent(content)

	// Limit content length
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}

	return content, nil
}

func extractText(selection *goquery.Selection) string {
	var text strings.Builder

	selection.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len(t) > 20 {
			text.WriteString(t)
			text.WriteString("\n\n")
		}
	})

	if text.Len() > 200 {
		return text.String()
	}

	// Fallback: extract text from leaf nodes (elements with no children)
	text.Reset()
	selection.Find("*").Each(func(i int, s *goquery.Selection) {
		if s.Children().Length() == 0 {
			t := strings.TrimSpace(s.Text())
			if len(t) > 20 {
				text.WriteString(t)
				text.WriteString(" ")
			}
		}
	})

	return strings.TrimSpace(text.String())
}

func cleanContent(content string) string {
	// Remove excessive whitespace
	re := regexp.MustCompile(`\n{3,}`)
	content = re.ReplaceAllString(content, "\n\n")

	// Remove multiple spaces
	re = regexp.MustCompile(` {2,}`)
	content = re.ReplaceAllString(content, " ")

	// Remove common junk patterns
	junkPatterns := []string{
		"Subscribe to our newsletter",
		"Sign up for",
		"Follow us on",
		"Share this article",
		"Read more:",
		"Click here to",
	}

	for _, pattern := range junkPatterns {
		content = strings.ReplaceAll(content, pattern, "")
	}

	return strings.TrimSpace(content)
}

// FetchContentFromReader extracts content from an io.Reader (for testing or reuse)
func (f *ContentFetcher) FetchContentFromReader(r io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", err
	}

	doc.Find("script, style, nav, header, footer, aside").Remove()

	var content string
	doc.Find("article, main, .content, .post, #content").Each(func(i int, s *goquery.Selection) {
		if content == "" {
			content = extractText(s)
		}
	})

	if content == "" {
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if len(text) > 50 {
				content += text + "\n\n"
			}
		})
	}

	return cleanContent(content), nil
}

// StripHTML removes HTML tags from a string, returning plain text.
// Used to sanitize RSS description/content fields that may contain HTML markup.
func StripHTML(html string) string {
	if !strings.Contains(html, "<") {
		return html
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	text := doc.Text()
	return cleanContent(text)
}
