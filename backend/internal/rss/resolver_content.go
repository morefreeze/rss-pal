package rss

import (
	"net/url"
	"strings"
)

var csdnReservedNames = map[string]struct{}{
	"article":   {},
	"community": {},
	"download":  {},
	"nav":       {},
	"rank":      {},
}

var githubReservedNames = map[string]struct{}{
	"about":            {},
	"account":          {},
	"apps":             {},
	"collections":      {},
	"customer-stories": {},
	"dashboard":        {},
	"enterprise":       {},
	"events":           {},
	"explore":          {},
	"features":         {},
	"issues":           {},
	"login":            {},
	"marketplace":      {},
	"new":              {},
	"notifications":    {},
	"organizations":    {},
	"orgs":             {},
	"pricing":          {},
	"pulls":            {},
	"search":           {},
	"security":         {},
	"settings":         {},
	"site":             {},
	"sponsors":         {},
	"topics":           {},
	"trending":         {},
	"users":            {},
}

func resolveCSDN(u *url.URL) (string, bool) {
	if canonicalHost(u) != "blog.csdn.net" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) == 0 {
		return "", false
	}
	if _, reserved := csdnReservedNames[strings.ToLower(parts[0])]; reserved {
		return "", false
	}
	user, ok := safePathSegment(parts[0])
	if !ok {
		return "", false
	}
	return "/csdn/blog/" + user, true
}

func resolveGitHub(u *url.URL) (string, bool) {
	if canonicalHost(u) != "github.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) == 0 {
		return "", false
	}
	if _, reserved := githubReservedNames[strings.ToLower(parts[0])]; reserved {
		return "", false
	}
	owner, ok := safePathSegment(parts[0])
	if !ok {
		return "", false
	}
	if len(parts) == 1 {
		return "/github/activity/" + owner, true
	}
	repoName := strings.TrimSuffix(parts[1], ".git")
	repo, ok := safePathSegment(repoName)
	if !ok {
		return "", false
	}
	return "/github/repo_event/" + owner + "/" + repo, true
}
