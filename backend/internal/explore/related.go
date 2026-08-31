package explore

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// RelatedSiteDiscoverer finds public feeds and a small, bounded set of sites
// linked from an already-fetched public page. It does not fetch or validate.
type RelatedSiteDiscoverer struct{}

func (RelatedSiteDiscoverer) Discover(pageURL string, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}
	base, ok := normalizePublicURL(pageURL)
	if !ok {
		return nil, nil
	}
	baseURL, _ := url.Parse(base)
	baseHost := hostOf(base)
	declared := newCandidateCollector(20, func(candidate Candidate) string { return candidate.FeedURL })
	external := newCandidateCollector(10, func(candidate Candidate) string { return candidate.ExternalKey })
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
				declared.add(Candidate{ExternalKey: resolved, FeedURL: resolved, SiteURL: base, Title: baseHost, OccurrenceCount: 1})
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
			external.add(Candidate{ExternalKey: domain, FeedURL: resolved, SiteURL: resolved, Title: domain, OccurrenceCount: 1})
		}
	}
	return append(declared.candidates(), external.candidates()...), nil
}

func isFeedDeclaration(token html.Token) bool {
	rel, hasRel := htmlAttribute(token, "rel")
	typeValue, hasType := htmlAttribute(token, "type")
	if !hasRel || !hasType || !hasASCIIToken(rel, "alternate") {
		return false
	}
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	return typeValue == "application/rss+xml" || typeValue == "application/atom+xml"
}

func hasASCIIToken(value, want string) bool {
	for start := 0; start < len(value); {
		for start < len(value) && isASCIIWhitespace(value[start]) {
			start++
		}
		end := start
		for end < len(value) && !isASCIIWhitespace(value[end]) {
			end++
		}
		if start < end && strings.EqualFold(value[start:end], want) {
			return true
		}
		start = end
	}
	return false
}

func isASCIIWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
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
