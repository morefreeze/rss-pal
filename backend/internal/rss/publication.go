package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ExtractPublishedAt reads explicit publication metadata before content cleanup.
// It never uses modification dates, URL dates, or arbitrary dates in the body.
func ExtractPublishedAt(doc *goquery.Document, pageURL string) *time.Time {
	location := time.UTC
	if u, err := url.Parse(pageURL); err == nil && u.Hostname() == "mp.weixin.qq.com" {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	parse := func(raw string) *time.Time { return parsePublicationDate(raw, location) }
	for _, selector := range []string{
		`head meta[property="article:published_time"], head meta[name="article:published_time"]`,
		`head meta[itemprop="datePublished"], head meta[name="datePublished"]`,
		`head meta[name="pubdate"], head meta[name="publishdate"], head meta[name="publish_date"], head meta[name="publication_date"]`,
	} {
		var dates []*time.Time
		doc.Find(selector).Each(func(_ int, s *goquery.Selection) { dates = append(dates, parse(s.AttrOr("content", ""))) })
		if date := uniquePublicationDate(dates); date != nil {
			return date
		}
	}
	var dates []*time.Time
	doc.Find(`script`).Each(func(_ int, s *goquery.Selection) {
		if !strings.EqualFold(strings.TrimSpace(s.AttrOr("type", "")), "application/ld+json") || isSecondaryPublication(s) {
			return
		}
		var value any
		if json.Unmarshal([]byte(s.Text()), &value) == nil {
			collectPublicationDates(value, pageURL, parse, &dates, 0)
		}
	})
	if date := uniquePublicationDate(dates); date != nil {
		return date
	}
	dates = nil
	doc.Find(`[itemprop="datePublished"], time.published, .entry-date.published`).Each(func(_ int, s *goquery.Selection) {
		if isSecondaryPublication(s) || s.Closest(`.updated, .modified, [itemprop="dateModified"]`).Length() > 0 {
			return
		}
		if s.AttrOr("itemprop", "") != "datePublished" {
			label := strings.ToLower(s.Parent().Text())
			if strings.Contains(label, "updated") || strings.Contains(label, "modified") || strings.Contains(label, "更新") {
				return
			}
		}
		raw := s.AttrOr("datetime", s.AttrOr("content", strings.TrimSpace(s.Text())))
		dates = append(dates, parse(raw))
	})
	if date := uniquePublicationDate(dates); date != nil {
		return date
	}
	if location != time.UTC {
		node := doc.Find("#publish_time").First()
		if !isSecondaryPublication(node) {
			if date := parse(node.Text()); date != nil {
				return date
			}
		}
		// Browser extraction may retain this visible value outside js_content.
		// Keep it below all original publication metadata, including pubdate.
		return parse(doc.Find(`head meta[name="rsspal:wechat_publish_time"]`).First().AttrOr("content", ""))
	}
	return nil
}

func isSecondaryPublication(s *goquery.Selection) bool {
	return s.Closest(`aside, nav, footer, [class*="comment"], [id*="comment"], [itemprop="comment"], [itemtype$="/Comment"], [class*="related"], [class*="recommend"], [id*="related"], [id*="recommend"]`).Length() > 0
}

func uniquePublicationDate(dates []*time.Time) *time.Time {
	var result *time.Time
	for _, date := range dates {
		if date == nil {
			continue
		}
		if result != nil && !result.Equal(*date) {
			return nil
		}
		result = date
	}
	return result
}

func parsePublicationDate(raw string, location *time.Location) *time.Time {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", time.RFC1123Z, time.RFC1123, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", "2006年1月2日 15:04", "2006年1月2日", "January 2, 2006", "Jan 2, 2006", "2 January 2006"} {
		date, err := time.ParseInLocation(layout, raw, location)
		if err == nil && date.Year() >= 1900 && !date.After(time.Now().Add(24*time.Hour)) {
			date = date.UTC()
			return &date
		}
	}
	return nil
}

// Traverse only document graph/mainEntity nodes. Recursing arbitrary keys can
// mistake a recommended article or a comment's publication date for this page.
func collectPublicationDates(value any, pageURL string, parse func(string) *time.Time, dates *[]*time.Time, depth int) {
	if depth > 12 {
		return
	}
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			collectPublicationDates(child, pageURL, parse, dates, depth+1)
		}
	case map[string]any:
		if publicationEntityType(v["@type"]) && publicationEntityMatches(v, pageURL) {
			if raw, ok := v["datePublished"].(string); ok {
				*dates = append(*dates, parse(raw))
			}
		}
		for _, key := range []string{"@graph", "mainEntity"} {
			collectPublicationDates(v[key], pageURL, parse, dates, depth+1)
		}
	}
}

func publicationEntityType(value any) bool {
	if values, ok := value.([]any); ok {
		for _, v := range values {
			if publicationEntityType(v) {
				return true
			}
		}
	}
	typ, _ := value.(string)
	typ = strings.TrimPrefix(strings.TrimPrefix(typ, "https://schema.org/"), "http://schema.org/")
	switch typ {
	case "Article", "BlogPosting", "NewsArticle", "TechArticle", "ScholarlyArticle", "Report":
		return true
	}
	return false
}

func publicationEntityMatches(entity map[string]any, pageURL string) bool {
	base, err := url.Parse(pageURL)
	if err != nil || base.Host == "" {
		return true
	}
	for _, key := range []string{"url", "mainEntityOfPage", "@id"} {
		value := entity[key]
		if object, ok := value.(map[string]any); ok {
			value = object["@id"]
		}
		if raw, ok := value.(string); ok && raw != "" {
			other, err := url.Parse(raw)
			if err != nil {
				return false
			}
			other = base.ResolveReference(other)
			if !strings.EqualFold(base.Host, other.Host) || strings.TrimRight(base.Path, "/") != strings.TrimRight(other.Path, "/") || base.RawQuery != other.RawQuery {
				return false
			}
		}
	}
	return true
}

// FetchPublicationDate is a bounded metadata-only request for operator repair.
// It uses the same SSRF-guarded client as normal content fetches, with no AI or
// third-party reader fallback and no content mutation.
func (f *ContentFetcher) FetchPublicationDate(ctx context.Context, pageURL string) (*time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
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
		return nil, fmt.Errorf("source HTTP %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml") {
		return nil, fmt.Errorf("source is not HTML: %s", ct)
	}
	const maxHTMLBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTMLBytes {
		return nil, fmt.Errorf("source HTML exceeds %d bytes", maxHTMLBytes)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	return ExtractPublishedAt(doc, pageURL), nil
}
