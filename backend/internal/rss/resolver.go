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
	resolveCSDN,
	resolveGitHub,
}

// ResolveFeedURL maps a user-facing platform URL to a native Feed or to an
// RSSHub route. Unknown and incomplete URLs are returned unchanged.
func ResolveFeedURL(input, rsshubBase string) string {
	resolved, _ := resolveFeedURL(input, rsshubBase)
	return resolved
}

// resolveFeedURL also reports whether the result is an RSSHub target derived
// from a supported public-platform URL. Callers may use that provenance to
// select the explicitly trusted RSSHub transport; a direct RSSHub URL never
// receives that trust bit.
func resolveFeedURL(input, rsshubBase string) (string, bool) {
	if rsshubBase == "" || input == "" {
		return input, false
	}

	u, err := url.Parse(input)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return input, false
	}
	if isRSSHubURL(u, rsshubBase) {
		return normalizeRSSHubWeiboUserURL(u, rsshubBase), false
	}
	if native, ok := resolveNativeFeed(u); ok {
		return native, false
	}
	for _, resolve := range rssHubResolvers {
		if route, ok := resolve(u); ok {
			return joinRSSHubURL(rsshubBase, route), true
		}
	}
	return input, false
}

func normalizeRSSHubWeiboUserURL(u *url.URL, rsshubBase string) string {
	b, err := url.Parse(rsshubBase)
	if err != nil || b.Host == "" || !strings.EqualFold(u.Scheme, b.Scheme) || !strings.EqualFold(u.Host, b.Host) {
		return u.String()
	}
	basePath := strings.TrimRight(b.Path, "/")
	path := strings.TrimRight(u.Path, "/")
	prefix := basePath + "/weibo/user/"
	if !strings.HasPrefix(path, prefix) {
		return u.String()
	}
	uid := strings.TrimPrefix(path, prefix)
	if !isDigits(uid) {
		return u.String()
	}
	if strings.HasSuffix(path, weiboCommentsPathSuffix) {
		return u.String()
	}
	u.Path = path + weiboCommentsPathSuffix
	return u.String()
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
