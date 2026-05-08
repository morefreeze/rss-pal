package transcript

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
)

// docFetcher is the slice of ContentFetcher's surface area we need.
// Defined as an interface so tests can stub it.
type docFetcher interface {
	FetchHTMLDocument(ctx context.Context, url string) (*goquery.Document, error)
}

// HTMLPageScraper looks for a transcript on the article's source HTML page
// using three sub-strategies in priority order: inline transcript section,
// linked .vtt/.srt/.txt files, and a two-hop "Find a transcript at: <URL>"
// announcement pattern.
type HTMLPageScraper struct {
	Docs       docFetcher
	HTTPClient *http.Client // for fetching linked subtitle files
}

const inlineMinChars = 200

var (
	transcriptHeadingRe = regexp.MustCompile(`(?i)\b(transcript|字幕|逐字稿)\b`)
	announceRe          = regexp.MustCompile(`(?i)find\s+(?:a\s+)?transcript.*?(https?://\S+)`)
	subtitleExtRe       = regexp.MustCompile(`(?i)\.(vtt|srt|txt)(?:[?#].*)?$`)
)

func (f *HTMLPageScraper) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.URL == "" {
		return nil, nil
	}
	if !strings.HasPrefix(article.MediaType, "audio/") && !strings.HasPrefix(article.MediaType, "video/") {
		return nil, nil
	}
	doc, err := f.Docs.FetchHTMLDocument(ctx, article.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch html: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	host := hostOf(article.URL)

	if text := findInlineTranscript(doc); text != "" {
		return &Result{Text: text, Source: host + " 网页字幕"}, nil
	}

	if text, src := f.tryLinkedSubtitle(ctx, doc, article.URL); text != "" {
		return &Result{Text: text, Source: src}, nil
	}

	if next := findAnnouncedTranscriptURL(doc); next != "" {
		nextDoc, err := f.Docs.FetchHTMLDocument(ctx, next)
		if err == nil && nextDoc != nil {
			if text := findInlineTranscript(nextDoc); text != "" {
				return &Result{Text: text, Source: hostOf(next) + " 网页字幕"}, nil
			}
			if text, src := f.tryLinkedSubtitle(ctx, nextDoc, next); text != "" {
				return &Result{Text: text, Source: src}, nil
			}
		}
	}
	return nil, nil
}

func findInlineTranscript(doc *goquery.Document) string {
	var found string
	doc.Find("h1, h2, h3, h4").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		if !transcriptHeadingRe.MatchString(h.Text()) {
			return true
		}
		var b strings.Builder
		for s := h.Next(); s.Length() > 0; s = s.Next() {
			tag := goquery.NodeName(s)
			if tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" {
				break
			}
			text := strings.TrimSpace(s.Text())
			if text == "" {
				continue
			}
			b.WriteString(text)
			b.WriteString("\n\n")
		}
		text := strings.TrimSpace(b.String())
		if len([]rune(text)) >= inlineMinChars {
			found = text
			return false
		}
		return true
	})
	return found
}

func (f *HTMLPageScraper) tryLinkedSubtitle(ctx context.Context, doc *goquery.Document, baseURL string) (string, string) {
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	var foundText, foundSource string
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		text := a.Text()
		hrefHasExt := subtitleExtRe.MatchString(href)
		looksLikeTranscript := transcriptHeadingRe.MatchString(text) || transcriptHeadingRe.MatchString(href)
		if !hrefHasExt {
			return true
		}
		if !looksLikeTranscript {
			return true
		}
		abs := resolveURL(baseURL, href)
		body, err := fetchString(ctx, client, abs)
		if err != nil {
			return true
		}
		if len(body) > 1<<20 {
			body = body[:1<<20]
		}
		parsed := strings.TrimSpace(ParseSubtitleFile(abs, body))
		if parsed == "" {
			return true
		}
		foundText = parsed
		foundSource = hostOf(baseURL) + " 字幕文件"
		return false
	})
	return foundText, foundSource
}

func findAnnouncedTranscriptURL(doc *goquery.Document) string {
	body := doc.Find("body").Text()
	m := announceRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func resolveURL(base, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	bu, err := url.Parse(base)
	if err != nil {
		return href
	}
	hu, err := url.Parse(href)
	if err != nil {
		return href
	}
	return bu.ResolveReference(hu).String()
}
