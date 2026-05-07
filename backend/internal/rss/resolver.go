package rss

import (
	"net/url"
	"regexp"
	"strings"
)

// bilibiliUserPathRe matches "/<uid>" or "/<uid>/video" on space.bilibili.com.
// <uid> must be one or more digits.
var bilibiliUserPathRe = regexp.MustCompile(`^/(\d+)(?:/video/?)?/?$`)

// ResolveFeedURL maps user-facing source URLs that don't expose RSS
// directly (e.g. Bilibili UP主 spaces) to their RSSHub equivalent on
// rsshubBase. Returns the input unchanged when no mapping applies, so
// it's safe to call on every URL the system handles.
//
// rsshubBase should be a scheme+host, no trailing slash (e.g.
// "http://rsshub:1200"). When rsshubBase is empty, the input is
// returned unchanged — i.e. no resolution attempted.
func ResolveFeedURL(input, rsshubBase string) string {
	if rsshubBase == "" || input == "" {
		return input
	}
	u, err := url.Parse(input)
	if err != nil || u.Host == "" {
		return input
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	if host == "space.bilibili.com" {
		if m := bilibiliUserPathRe.FindStringSubmatch(u.Path); m != nil {
			return strings.TrimRight(rsshubBase, "/") + "/bilibili/user/video/" + m[1]
		}
	}
	return input
}
