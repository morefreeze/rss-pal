package rss

import (
	"net/url"
	"strings"
)

const weiboCommentsPathSuffix = "/displayComments=1"

func resolveWeibo(u *url.URL) (string, bool) {
	host := canonicalHost(u)
	parts := pathSegments(u)
	if len(parts) != 2 || !isDigits(parts[1]) {
		return "", false
	}
	if host == "weibo.com" && parts[0] == "u" {
		return "/weibo/user/" + parts[1] + weiboCommentsPathSuffix, true
	}
	if host == "m.weibo.cn" && (parts[0] == "u" || parts[0] == "profile") {
		return "/weibo/user/" + parts[1] + weiboCommentsPathSuffix, true
	}
	return "", false
}

func weiboCommentsFallbackURL(raw, rsshubBase string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Path, weiboCommentsPathSuffix) {
		return "", false
	}
	base, err := url.Parse(rsshubBase)
	if err != nil || base.Host == "" || !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	fallbackPath := strings.TrimSuffix(u.Path, weiboCommentsPathSuffix)
	routePrefix := strings.TrimRight(base.Path, "/") + "/weibo/user/"
	if !strings.HasPrefix(fallbackPath, routePrefix) || !isDigits(strings.TrimPrefix(fallbackPath, routePrefix)) {
		return "", false
	}
	u.Path = fallbackPath
	return u.String(), true
}

func resolveZhihu(u *url.URL) (string, bool) {
	if canonicalHost(u) != "zhihu.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) == 2 && parts[0] == "people" {
		id, ok := safePathSegment(parts[1])
		if ok {
			return "/zhihu/people/activities/" + id, true
		}
	}
	if len(parts) == 3 && parts[0] == "people" && (parts[2] == "activities" || parts[2] == "answers") {
		id, ok := safePathSegment(parts[1])
		if ok {
			return "/zhihu/people/" + parts[2] + "/" + id, true
		}
	}
	if len(parts) == 2 && (parts[0] == "question" || parts[0] == "topic") && isDigits(parts[1]) {
		id, ok := safePathSegment(parts[1])
		if ok {
			return "/zhihu/" + parts[0] + "/" + id, true
		}
	}
	return "", false
}

func resolveWeChat(u *url.URL) (string, bool) {
	if canonicalHost(u) != "mp.weixin.qq.com" || u.Path != "/mp/homepage" {
		return "", false
	}
	query := u.Query()
	biz, bizOK := safePathSegment(query.Get("__biz"))
	hid, hidOK := safePathSegment(query.Get("hid"))
	if !bizOK || !hidOK {
		return "", false
	}
	route := "/wechat/mp/homepage/" + biz + "/" + hid
	if cid := query.Get("cid"); cid != "" {
		encoded, ok := safePathSegment(cid)
		if !ok {
			return "", false
		}
		route += "/" + encoded
	}
	return route, true
}

func resolveXiaohongshu(u *url.URL) (string, bool) {
	host := canonicalHost(u)
	if host != "xiaohongshu.com" && host != "m.xiaohongshu.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) != 3 || parts[0] != "user" || parts[1] != "profile" {
		return "", false
	}
	id, ok := safePathSegment(parts[2])
	if !ok {
		return "", false
	}
	return "/xiaohongshu/user/" + id + "/notes", true
}
