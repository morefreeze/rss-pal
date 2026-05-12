package rss

import (
	"net/url"
	"regexp"
	"strings"
)

// twitterHosts is the set of canonical and legacy hosts that serve Twitter /
// X content. Mobile and www subdomains are accepted because users land on
// them depending on the device and the link source.
var twitterHosts = map[string]struct{}{
	"x.com":              {},
	"www.x.com":          {},
	"twitter.com":        {},
	"www.twitter.com":    {},
	"mobile.twitter.com": {},
}

// twitterStatusPathRe matches a single-tweet permalink path. The handle is
// limited to Twitter's documented 15-char max plus `_` to avoid swallowing
// internal `/i/...` system routes.
var twitterStatusPathRe = regexp.MustCompile(`^/[A-Za-z0-9_]{1,15}/status/([0-9]+)/?$`)

// IsTwitterStatusURL reports whether raw is a Twitter / X single-tweet
// permalink. On match it returns the numeric tweet id from the path so the
// caller can pin DOM extraction to the focal tweet (a tweet page renders
// many tweets in the conversation; only one matches the URL).
func IsTwitterStatusURL(raw string) (statusID string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if _, ok := twitterHosts[host]; !ok {
		return "", false
	}
	m := twitterStatusPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return m[1], true
}
