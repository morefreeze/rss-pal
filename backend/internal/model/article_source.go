package model

import (
	"net/url"
	"strings"
)

// ArticleSource describes where the article was published, independently of
// the subscription feed that discovered it and controls ownership.
type ArticleSource struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ResolveSource applies to both historical and newly fetched link-set children.
// The original URL and parent relation are authoritative; storing another copy
// of the host would become stale after URL repairs or redirects.
func (a *Article) ResolveSource() {
	a.Source = nil
	if a.ParentArticleID == nil {
		return
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	a.Source = &ArticleSource{Key: "host:" + host, Title: host, URL: a.URL}
}
