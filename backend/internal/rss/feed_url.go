package rss

import (
	"net/url"
	"strings"
)

// CanonicalFeedURL upgrades FeedBurner's legacy HTTP origin to its HTTPS
// equivalent. Preserve the feed's case-sensitive path and raw query; this is
// identity normalization, not validation or permission to use a trusted client.
// Explicit nondefault ports and malformed/credential-bearing URLs stay intact
// so the normal public-URL checks still handle them.
func CanonicalFeedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, "http") ||
		!strings.EqualFold(u.Hostname(), "feeds.feedburner.com") ||
		strings.HasSuffix(u.Host, ":") || (u.Port() != "" && u.Port() != "80") {
		return raw
	}
	u.Scheme, u.Host = "https", "feeds.feedburner.com"
	return u.String()
}
