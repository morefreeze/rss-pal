package rss

import (
	"net/url"
	"strings"
)

// IsNetworkFeedURL excludes internal sentinels and malformed legacy imports.
func IsNetworkFeedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return !strings.Contains(u.Path, "://") && !strings.HasSuffix(u.Host, ":")
}
