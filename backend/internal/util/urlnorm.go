// Package util holds small pure helpers shared across the codebase.
package util

import (
	"net/url"
	"regexp"
	"strings"
)

var trackingParamsExact = map[string]struct{}{
	"gclid":   {},
	"fbclid":  {},
	"msclkid": {},
	"yclid":   {},
	"igshid":  {},
	"mc_cid":  {},
	"mc_eid":  {},
	"_hsenc":  {},
	"_hsmi":   {},
	"ref":     {},
	"ref_src": {},
}

// NormalizeURL returns a canonical form of raw used for article deduplication.
// Strips #fragment, removes tracking parameters (utm_*, gclid, fbclid, ...),
// lowercases the host. Preserves scheme, path case, and trailing slash so it
// stays compatible with already-stored URLs from RSS feeds.
//
// Inputs that fail to parse are returned unchanged so the caller can still
// match exotic URLs by exact-string equality.
func NormalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}

	u.Fragment = ""
	u.RawFragment = ""

	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if _, drop := trackingParamsExact[k]; drop || strings.HasPrefix(k, "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}

	u.Host = strings.ToLower(u.Host)

	// Twitter / X canonicalization: collapse legacy hosts onto x.com, then
	// strip share-tracking query (`?s=20`, `?t=...`) from single-tweet
	// permalinks. Tweet IDs are globally unique, so the query carries no
	// content — only attribution noise that would otherwise split dedup.
	switch u.Host {
	case "twitter.com", "www.twitter.com", "mobile.twitter.com", "www.x.com":
		u.Host = "x.com"
	}
	if u.Host == "x.com" && twitterStatusPathRe.MatchString(u.Path) {
		u.RawQuery = ""
	}

	return u.String()
}

// twitterStatusPathRe matches /<handle>/status/<numeric_id> with optional
// trailing slash. Used by NormalizeURL to recognize single-tweet permalinks
// whose query string is always pure tracking. Kept here (not in package rss)
// because url normalization is a util concern.
var twitterStatusPathRe = regexp.MustCompile(`^/[A-Za-z0-9_]{1,15}/status/[0-9]+/?$`)
