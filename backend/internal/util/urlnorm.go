// Package util holds small pure helpers shared across the codebase.
package util

import (
	"net/url"
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
	return u.String()
}
