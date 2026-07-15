package rss

import (
	"net/url"
	"strings"
	"unicode"
)

type platformResolver func(*url.URL) (string, bool)

var rssHubResolvers = []platformResolver{
	resolveBilibili,
	resolveYouTube,
	resolveDouyin,
	resolveTikTok,
	resolveWeibo,
	resolveZhihu,
	resolveWeChat,
	resolveXiaohongshu,
}

// ResolveFeedURL maps a user-facing platform URL to a native Feed or to an
// RSSHub route. Unknown and incomplete URLs are returned unchanged.
func ResolveFeedURL(input, rsshubBase string) string {
	if rsshubBase == "" || input == "" {
		return input
	}

	u, err := url.Parse(input)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return input
	}
	if isRSSHubURL(u, rsshubBase) {
		return input
	}
	if native, ok := resolveNativeFeed(u); ok {
		return native
	}
	for _, resolve := range rssHubResolvers {
		if route, ok := resolve(u); ok {
			return joinRSSHubURL(rsshubBase, route)
		}
	}
	return input
}

func canonicalHost(u *url.URL) string {
	return strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
}

func pathSegments(u *url.URL) []string {
	raw := strings.Trim(u.Path, "/")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func safePathSegment(value string) (string, bool) {
	if value == "" || value == "." || value == ".." {
		return "", false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", false
		}
	}
	return url.PathEscape(value), true
}

func isDigits(value string) bool {
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

func joinRSSHubURL(base, route string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(route, "/")
}

func isRSSHubURL(u *url.URL, base string) bool {
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, b.Scheme) && strings.EqualFold(u.Host, b.Host)
}
