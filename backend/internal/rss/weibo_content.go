package rss

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const bloggerCommentHeading = "### 博主首评"

// BuildItemContent normalizes an RSS item's description while retaining the
// existing plain-text behavior for non-Weibo items.
func BuildItemContent(description, fallback, itemURL string) (content string, enriched bool) {
	raw := description
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}

	uid, ok := desktopWeiboStatusUID(itemURL)
	if !ok {
		return StripHTML(raw), false
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return StripHTML(raw), false
	}

	rewriteWeiboRedirectAnchors(doc.Selection)
	wrappers := findHotCommentsWrappers(doc.Selection)
	if wrappers.Length() == 0 {
		return cleanContent(ExtractMarkdown(doc.Selection)), false
	}

	var comment *goquery.Selection
	wrappers.Each(func(_ int, wrapper *goquery.Selection) {
		if comment == nil {
			comment = firstBloggerComment(wrapper, uid)
		}
	})
	wrappers.Remove()
	body := cleanContent(ExtractMarkdown(doc.Selection))
	if comment == nil {
		return body, false
	}

	commentMarkdown := cleanContent(ExtractMarkdown(comment))
	if commentMarkdown == "" {
		return body, false
	}
	if body == "" {
		return cleanContent(bloggerCommentHeading + "\n\n" + commentMarkdown), true
	}
	return cleanContent(body + "\n\n" + bloggerCommentHeading + "\n\n" + commentMarkdown), true
}

// ShouldDeepFetchArticle reports whether an RSS item should be fetched from
// its linked page for a fuller article body.
func ShouldDeepFetchArticle(feedType, itemURL, mediaType string) bool {
	if feedType == "youtube" || feedType == "podcast" || strings.HasPrefix(mediaType, "video/") {
		return false
	}
	if _, ok := desktopWeiboStatusUID(itemURL); ok {
		return false
	}
	return !isMobileWeiboStatusURL(itemURL)
}

func desktopWeiboStatusUID(rawURL string) (string, bool) {
	u, ok := parseHTTPURL(rawURL)
	if !ok || u.Scheme != "https" || !strings.EqualFold(u.Host, "weibo.com") {
		return "", false
	}
	parts := urlPathParts(u.Path)
	if len(parts) != 2 || !isNumeric(parts[0]) || parts[1] == "" {
		return "", false
	}
	return parts[0], true
}

func isMobileWeiboStatusURL(rawURL string) bool {
	u, ok := parseHTTPURL(rawURL)
	if !ok || !strings.EqualFold(u.Host, "m.weibo.cn") {
		return false
	}
	parts := urlPathParts(u.Path)
	return len(parts) == 2 && parts[0] == "status" && parts[1] != ""
}

func parseHTTPURL(rawURL string) (*url.URL, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, false
	}
	return u, true
}

func urlPathParts(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func findHotCommentsWrappers(root *goquery.Selection) *goquery.Selection {
	return root.Find("h3").FilterFunction(func(_ int, heading *goquery.Selection) bool {
		return strings.TrimSpace(heading.Text()) == "热门评论"
	}).Parent()
}

func firstBloggerComment(wrapper *goquery.Selection, uid string) *goquery.Selection {
	var comment *goquery.Selection
	wrapper.ChildrenFiltered("p").EachWithBreak(func(_ int, paragraph *goquery.Selection) bool {
		candidate := paragraph.Clone()
		candidate.Find("blockquote").Remove()
		author := candidate.ChildrenFiltered("a[href]").First()
		if !anchorPointsToWeiboUID(author, uid) {
			return true
		}
		comment = candidate
		return false
	})
	return comment
}

func anchorPointsToWeiboUID(anchor *goquery.Selection, uid string) bool {
	href, exists := anchor.Attr("href")
	if !exists {
		return false
	}
	u, err := url.Parse(href)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "weibo.com") {
		return false
	}
	parts := urlPathParts(u.Path)
	return len(parts) == 1 && parts[0] == uid
}

func rewriteWeiboRedirectAnchors(root *goquery.Selection) {
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		redirect, err := url.Parse(href)
		if err != nil || redirect.Scheme != "https" || !strings.EqualFold(redirect.Host, "weibo.cn") || redirect.Path != "/sinaurl" {
			return
		}

		target := redirect.Query().Get("u")
		direct, err := url.Parse(target)
		if err != nil || !direct.IsAbs() || (direct.Scheme != "http" && direct.Scheme != "https") || direct.Hostname() == "" {
			return
		}
		anchor.SetAttr("href", direct.String())
	})
}
