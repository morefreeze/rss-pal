package rss

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// BestBlogs uses one article element per recommendation, rather than one for
// the whole issue. Selecting the first article loses the introduction and all
// subsequent recommendations. Keep the editorial blocks in document order,
// without the subscription, podcast-player, or table-of-contents controls.
func bestBlogsNewsletterContent(doc *goquery.Document) *goquery.Selection {
	canonical := doc.Find(`link[rel="canonical"]`).First().AttrOr("href", "")
	u, err := url.Parse(canonical)
	if err != nil || (u.Hostname() != "www.bestblogs.dev" && u.Hostname() != "bestblogs.dev") {
		return nil
	}
	path := strings.TrimPrefix(u.Path, "/en")
	path = strings.TrimPrefix(path, "/zh")
	if !strings.HasPrefix(path, "/newsletter/issue") {
		return nil
	}
	main := doc.Find("main").First()
	if main.Find("article h2").Length() == 0 || main.Find(".markdown-content").Length() == 0 {
		return nil
	}
	container, _ := goquery.NewDocumentFromReader(strings.NewReader("<html><body></body></html>"))
	body := container.Find("body")
	main.Find("h1, .markdown-content, article").Each(func(_ int, block *goquery.Selection) {
		// An article already includes its own Markdown description.
		if block.ParentsFiltered("article, .markdown-content").Length() == 0 {
			body.AppendSelection(block.Clone())
		}
	})
	return body
}
