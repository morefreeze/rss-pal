package explore

import (
	"bytes"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// RelatedSiteDiscoverer finds public feeds and a small, bounded set of sites
// linked from an already-fetched public page. It does not fetch or validate.
type RelatedSiteDiscoverer struct{}

func (RelatedSiteDiscoverer) Discover(pageURL string, body []byte) ([]Candidate, error) {
	base, ok := normalizePublicURL(pageURL)
	if !ok {
		return nil, nil
	}
	baseURL, _ := url.Parse(base)
	baseHost := hostOf(base)
	declared, external := []Candidate{}, map[string]Candidate{}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		switch strings.ToLower(token.Data) {
		case "link":
			if !isFeedDeclaration(token) {
				continue
			}
			href, found := htmlAttribute(token, "href")
			if !found {
				continue
			}
			if resolved, ok := resolvePublicURL(baseURL, href); ok {
				declared = append(declared, Candidate{ExternalKey: resolved, FeedURL: resolved, SiteURL: base, Title: baseHost})
			}
		case "a":
			href, found := htmlAttribute(token, "href")
			if !found {
				continue
			}
			resolved, ok := resolvePublicURL(baseURL, href)
			if !ok || hostOf(resolved) == baseHost || ignoredRelatedURL(resolved) {
				continue
			}
			domain := hostOf(resolved)
			if _, exists := external[domain]; !exists {
				external[domain] = Candidate{ExternalKey: domain, FeedURL: resolved, SiteURL: resolved, Title: domain}
			}
		}
	}
	declared = NormalizeCandidates(declared)
	list := make([]Candidate, 0, len(external))
	for _, candidate := range external {
		list = append(list, candidate)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].FeedURL < list[j].FeedURL })
	if len(list) > 10 {
		list = list[:10]
	}
	return append(declared, list...), nil
}

func isFeedDeclaration(token html.Token) bool {
	rel, hasRel := htmlAttribute(token, "rel")
	typeValue, hasType := htmlAttribute(token, "type")
	if !hasRel || !hasType || !strings.Contains(strings.ToLower(rel), "alternate") {
		return false
	}
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	return typeValue == "application/rss+xml" || typeValue == "application/atom+xml"
}

func htmlAttribute(token html.Token, name string) (string, bool) {
	for _, attr := range token.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func resolvePublicURL(base *url.URL, raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return normalizePublicURL(base.ResolveReference(u).String())
}

func ignoredRelatedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	path := strings.ToLower(u.Path)
	for _, suffix := range []string{".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".mp4", ".webm", ".mp3", ".woff", ".woff2"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return isPrivateHost(hostOf(raw))
}
