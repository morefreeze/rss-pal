package rss

import (
	"net/url"
	"strings"
)

func resolveNativeFeed(u *url.URL) (string, bool) {
	if !isYouTubeHost(canonicalHost(u)) {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) >= 2 && parts[0] == "channel" && strings.HasPrefix(parts[1], "UC") {
		if _, ok := safePathSegment(parts[1]); !ok {
			return "", false
		}
		return "https://www.youtube.com/feeds/videos.xml?channel_id=" + url.QueryEscape(parts[1]), true
	}
	if len(parts) == 1 && parts[0] == "playlist" {
		id := u.Query().Get("list")
		if _, ok := safePathSegment(id); !ok {
			return "", false
		}
		return "https://www.youtube.com/feeds/videos.xml?playlist_id=" + url.QueryEscape(id), true
	}
	return "", false
}

func resolveBilibili(u *url.URL) (string, bool) {
	if canonicalHost(u) != "space.bilibili.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) < 1 || len(parts) > 2 || !isDigits(parts[0]) {
		return "", false
	}
	if len(parts) == 2 && parts[1] != "video" {
		return "", false
	}
	return "/bilibili/user/video/" + parts[0], true
}

func resolveYouTube(u *url.URL) (string, bool) {
	if !isYouTubeHost(canonicalHost(u)) {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) == 1 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 {
		handle, ok := safePathSegment(parts[0])
		if !ok {
			return "", false
		}
		return "/youtube/user/" + handle, true
	}
	if len(parts) == 2 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 && parts[1] == "videos" {
		handle, ok := safePathSegment(parts[0])
		if !ok {
			return "", false
		}
		return "/youtube/user/" + handle, true
	}
	if len(parts) == 2 && (parts[0] == "user" || parts[0] == "c") {
		name, ok := safePathSegment(parts[1])
		if !ok {
			return "", false
		}
		return "/youtube/" + parts[0] + "/" + name, true
	}
	return "", false
}

func resolveDouyin(u *url.URL) (string, bool) {
	host := canonicalHost(u)
	parts := pathSegments(u)
	if host == "live.douyin.com" && len(parts) == 1 {
		rid, ok := safePathSegment(parts[0])
		if ok {
			return "/douyin/live/" + rid, true
		}
	}
	if host != "douyin.com" || len(parts) != 2 {
		return "", false
	}
	id, ok := safePathSegment(parts[1])
	if !ok {
		return "", false
	}
	switch parts[0] {
	case "user":
		if strings.HasPrefix(parts[1], "MS4wLjABAAAA") {
			return "/douyin/user/" + id, true
		}
	case "hashtag":
		return "/douyin/hashtag/" + id, true
	}
	return "", false
}

func resolveTikTok(u *url.URL) (string, bool) {
	if canonicalHost(u) != "tiktok.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) != 1 || !strings.HasPrefix(parts[0], "@") || len(parts[0]) == 1 {
		return "", false
	}
	user, ok := safePathSegment(parts[0])
	if !ok {
		return "", false
	}
	return "/tiktok/user/" + user, true
}

func isYouTubeHost(host string) bool {
	return host == "youtube.com" || host == "m.youtube.com"
}
