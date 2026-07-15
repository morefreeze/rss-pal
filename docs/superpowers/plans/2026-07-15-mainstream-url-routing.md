# Mainstream URL Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convert user-pasted mainstream platform URLs into deterministic native Feed or internal RSSHub URLs before Preview and scheduled fetching.

**Architecture:** Keep `ResolveFeedURL(input, rsshubBase string) string` as the single public entrypoint. Move platform-specific pure URL matchers into video, social, and content resolver files; prefer YouTube native feeds, preserve the original input when matching is incomplete, and let the existing Preview/Fetch call sites consume the resolved URL without API or persistence changes.

**Tech Stack:** Go 1.x, `net/url`, table-driven `testing`, existing `Fetcher` and RSSHub Compose sidecar.

---

### Task 1: Core resolver contract and video platforms

**Files:**
- Modify: `backend/internal/rss/resolver.go`
- Modify: `backend/internal/rss/resolver_test.go`
- Create: `backend/internal/rss/resolver_video.go`
- Create: `backend/internal/rss/resolver_video_test.go`

- [ ] **Step 1: Replace the core test with shared table helpers and write failing video-route tests**

Replace `backend/internal/rss/resolver_test.go` with:

```go
package rss

import "testing"

type resolveCase struct {
	name       string
	input      string
	rsshubBase string
	want       string
}

func runResolveCases(t *testing.T, cases []resolveCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFeedURL(tc.input, tc.rsshubBase); got != tc.want {
				t.Fatalf("ResolveFeedURL(%q, %q) = %q, want %q", tc.input, tc.rsshubBase, got, tc.want)
			}
		})
	}
}

func TestResolveFeedURLCore(t *testing.T) {
	runResolveCases(t, []resolveCase{
		{name: "empty_base", input: "https://www.youtube.com/channel/UC123", rsshubBase: "", want: "https://www.youtube.com/channel/UC123"},
		{name: "empty_input", input: "", rsshubBase: "http://rsshub:1200", want: ""},
		{name: "malformed_input", input: "://nope", rsshubBase: "http://rsshub:1200", want: "://nope"},
		{name: "relative_input", input: "/user/123", rsshubBase: "http://rsshub:1200", want: "/user/123"},
		{name: "non_http_scheme", input: "ftp://space.bilibili.com/14064034", rsshubBase: "http://rsshub:1200", want: "ftp://space.bilibili.com/14064034"},
		{name: "existing_rsshub_url", input: "http://rsshub:1200/weibo/user/1195230310", rsshubBase: "http://rsshub:1200/", want: "http://rsshub:1200/weibo/user/1195230310"},
		{name: "unmatched_host", input: "https://example.com/blog", rsshubBase: "http://rsshub:1200", want: "https://example.com/blog"},
	})
}
```

Create `backend/internal/rss/resolver_video_test.go`:

```go
package rss

import "testing"

func TestResolveFeedURLVideoPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "bilibili_user", input: "https://space.bilibili.com/14064034", rsshubBase: base, want: base + "/bilibili/user/video/14064034"},
		{name: "bilibili_user_video", input: "https://www.space.bilibili.com/14064034/video/?from=search#top", rsshubBase: base, want: base + "/bilibili/user/video/14064034"},
		{name: "bilibili_non_numeric", input: "https://space.bilibili.com/not-a-uid", rsshubBase: base, want: "https://space.bilibili.com/not-a-uid"},
		{name: "bilibili_dynamic", input: "https://space.bilibili.com/14064034/dynamic", rsshubBase: base, want: "https://space.bilibili.com/14064034/dynamic"},

		{name: "youtube_channel_native", input: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/videos", rsshubBase: base, want: "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q"},
		{name: "youtube_playlist_native", input: "https://youtube.com/playlist?list=PL123&utm_source=test", rsshubBase: base, want: "https://www.youtube.com/feeds/videos.xml?playlist_id=PL123"},
		{name: "youtube_handle", input: "https://youtube.com/@Fireship/videos?view=0", rsshubBase: base, want: base + "/youtube/user/@Fireship"},
		{name: "youtube_user", input: "https://m.youtube.com/user/GoogleDevelopers", rsshubBase: base, want: base + "/youtube/user/GoogleDevelopers"},
		{name: "youtube_custom", input: "https://www.youtube.com/c/Computerphile", rsshubBase: base, want: base + "/youtube/c/Computerphile"},
		{name: "youtube_watch_passthrough", input: "https://www.youtube.com/watch?v=abc", rsshubBase: base, want: "https://www.youtube.com/watch?v=abc"},
		{name: "youtube_shorts_passthrough", input: "https://www.youtube.com/shorts/abc", rsshubBase: base, want: "https://www.youtube.com/shorts/abc"},
		{name: "youtube_playlist_missing_id", input: "https://www.youtube.com/playlist", rsshubBase: base, want: "https://www.youtube.com/playlist"},

		{name: "douyin_user", input: "https://www.douyin.com/user/MS4wLjABAAAAexample?from_tab_name=main", rsshubBase: base, want: base + "/douyin/user/MS4wLjABAAAAexample"},
		{name: "douyin_hashtag", input: "https://www.douyin.com/hashtag/123456", rsshubBase: base, want: base + "/douyin/hashtag/123456"},
		{name: "douyin_live", input: "https://live.douyin.com/987654", rsshubBase: base, want: base + "/douyin/live/987654"},
		{name: "douyin_video_passthrough", input: "https://www.douyin.com/video/123", rsshubBase: base, want: "https://www.douyin.com/video/123"},
		{name: "douyin_invalid_user", input: "https://www.douyin.com/user/short-id", rsshubBase: base, want: "https://www.douyin.com/user/short-id"},

		{name: "tiktok_user", input: "https://www.tiktok.com/@linustech/", rsshubBase: base, want: base + "/tiktok/user/@linustech"},
		{name: "tiktok_video_passthrough", input: "https://www.tiktok.com/@linustech/video/123", rsshubBase: base, want: "https://www.tiktok.com/@linustech/video/123"},
		{name: "tiktok_live_passthrough", input: "https://www.tiktok.com/@linustech/live", rsshubBase: base, want: "https://www.tiktok.com/@linustech/live"},
	})
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd backend
go test -count=1 ./internal/rss -run 'TestResolveFeedURL(Core|VideoPlatforms)$'
```

Expected: FAIL on the first new YouTube, Douyin, or TikTok expectation because the existing implementation only resolves Bilibili; the non-HTTP Bilibili case must also fail because the current resolver does not check the scheme.

- [ ] **Step 3: Implement the core dispatcher and video resolvers**

Replace `backend/internal/rss/resolver.go` with:

```go
package rss

import (
	"net/url"
	"strings"
)

type platformResolver func(*url.URL) (string, bool)

var rssHubResolvers = []platformResolver{
	resolveBilibili,
	resolveYouTube,
	resolveDouyin,
	resolveTikTok,
}

// ResolveFeedURL maps a user-facing platform URL to a native Feed or to an
// RSSHub route. Unknown and incomplete URLs are returned unchanged.
func ResolveFeedURL(input, rsshubBase string) string {
	if rsshubBase == "" || input == "" {
		return input
	}

	u, err := url.Parse(input)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return input
	}
	if isRSSHubURL(u, rsshubBase) {
		return input
	}
	if native, ok := resolveNativeFeed(u); ok {
		return native
	}
	for _, resolve := range rssHubResolvers {
		if route, ok := resolve(u); ok {
			return joinRSSHubURL(rsshubBase, route)
		}
	}
	return input
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
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, " \t\r\n") {
		return "", false
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
```

Create `backend/internal/rss/resolver_video.go`:

```go
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
		handle, _ := safePathSegment(parts[0])
		return "/youtube/user/" + handle, true
	}
	if len(parts) == 2 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 && parts[1] == "videos" {
		handle, _ := safePathSegment(parts[0])
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
```

- [ ] **Step 4: Format and verify GREEN**

Run:

```bash
gofmt -w internal/rss/resolver.go internal/rss/resolver_test.go internal/rss/resolver_video.go internal/rss/resolver_video_test.go
go test -count=1 ./internal/rss -run 'TestResolveFeedURL(Core|VideoPlatforms)$'
```

Expected: PASS for both tests with no network access.

- [ ] **Step 5: Commit the video routing slice**

```bash
git add backend/internal/rss/resolver.go backend/internal/rss/resolver_test.go backend/internal/rss/resolver_video.go backend/internal/rss/resolver_video_test.go
git commit -m "feat: resolve mainstream video feed URLs"
```

### Task 2: Social platforms and WeChat homepage

**Files:**
- Modify: `backend/internal/rss/resolver.go`
- Create: `backend/internal/rss/resolver_social.go`
- Create: `backend/internal/rss/resolver_social_test.go`

- [ ] **Step 1: Write failing social-platform tests**

Create `backend/internal/rss/resolver_social_test.go`:

```go
package rss

import "testing"

func TestResolveFeedURLSocialPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "weibo_desktop", input: "https://weibo.com/u/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310"},
		{name: "weibo_mobile_u", input: "https://m.weibo.cn/u/1195230310?jumpfrom=weibocom", rsshubBase: base, want: base + "/weibo/user/1195230310"},
		{name: "weibo_mobile_profile", input: "https://m.weibo.cn/profile/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310"},
		{name: "weibo_non_numeric", input: "https://weibo.com/u/not-a-uid", rsshubBase: base, want: "https://weibo.com/u/not-a-uid"},
		{name: "weibo_status_passthrough", input: "https://weibo.com/1195230310/P123", rsshubBase: base, want: "https://weibo.com/1195230310/P123"},

		{name: "zhihu_people", input: "https://www.zhihu.com/people/diygod", rsshubBase: base, want: base + "/zhihu/people/activities/diygod"},
		{name: "zhihu_people_activities", input: "https://www.zhihu.com/people/diygod/activities", rsshubBase: base, want: base + "/zhihu/people/activities/diygod"},
		{name: "zhihu_people_answers", input: "https://www.zhihu.com/people/diygod/answers", rsshubBase: base, want: base + "/zhihu/people/answers/diygod"},
		{name: "zhihu_question", input: "https://www.zhihu.com/question/123456", rsshubBase: base, want: base + "/zhihu/question/123456"},
		{name: "zhihu_topic", input: "https://www.zhihu.com/topic/19550517", rsshubBase: base, want: base + "/zhihu/topic/19550517"},
		{name: "zhihu_answer_passthrough", input: "https://www.zhihu.com/question/123456/answer/789", rsshubBase: base, want: "https://www.zhihu.com/question/123456/answer/789"},
		{name: "zhihu_article_passthrough", input: "https://zhuanlan.zhihu.com/p/123", rsshubBase: base, want: "https://zhuanlan.zhihu.com/p/123"},

		{name: "wechat_homepage", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D&hid=16", rsshubBase: base, want: base + "/wechat/mp/homepage/MzA3MDM3NjE5NQ==/16"},
		{name: "wechat_homepage_category", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D&hid=16&cid=2", rsshubBase: base, want: base + "/wechat/mp/homepage/MzA3MDM3NjE5NQ==/16/2"},
		{name: "wechat_article_passthrough", input: "https://mp.weixin.qq.com/s/kHGSiyxTf8J4ZxmJLM2QJQ", rsshubBase: base, want: "https://mp.weixin.qq.com/s/kHGSiyxTf8J4ZxmJLM2QJQ"},
		{name: "wechat_missing_hid", input: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D", rsshubBase: base, want: "https://mp.weixin.qq.com/mp/homepage?__biz=MzA3MDM3NjE5NQ%3D%3D"},

		{name: "xiaohongshu_user", input: "https://www.xiaohongshu.com/user/profile/593032945e87e77791e03696?xsec_token=secret", rsshubBase: base, want: base + "/xiaohongshu/user/593032945e87e77791e03696/notes"},
		{name: "xiaohongshu_note_passthrough", input: "https://www.xiaohongshu.com/explore/123", rsshubBase: base, want: "https://www.xiaohongshu.com/explore/123"},
		{name: "xiaohongshu_missing_user", input: "https://www.xiaohongshu.com/user/profile/", rsshubBase: base, want: "https://www.xiaohongshu.com/user/profile/"},
	})
}
```

- [ ] **Step 2: Run the social test and verify RED**

Run:

```bash
cd backend
go test -count=1 ./internal/rss -run TestResolveFeedURLSocialPlatforms
```

Expected: FAIL on `weibo_desktop` because none of the social resolvers is registered yet.

- [ ] **Step 3: Implement social resolvers and register them**

Create `backend/internal/rss/resolver_social.go`:

```go
package rss

import "net/url"

func resolveWeibo(u *url.URL) (string, bool) {
	host := canonicalHost(u)
	parts := pathSegments(u)
	if len(parts) != 2 || !isDigits(parts[1]) {
		return "", false
	}
	if host == "weibo.com" && parts[0] == "u" {
		return "/weibo/user/" + parts[1], true
	}
	if host == "m.weibo.cn" && (parts[0] == "u" || parts[0] == "profile") {
		return "/weibo/user/" + parts[1], true
	}
	return "", false
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
	if len(parts) == 2 && (parts[0] == "question" || parts[0] == "topic") {
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
```

Update the resolver list in `backend/internal/rss/resolver.go` to exactly:

```go
var rssHubResolvers = []platformResolver{
	resolveBilibili,
	resolveYouTube,
	resolveDouyin,
	resolveTikTok,
	resolveWeibo,
	resolveZhihu,
	resolveWeChat,
	resolveXiaohongshu,
}
```

- [ ] **Step 4: Format and verify GREEN**

Run:

```bash
gofmt -w internal/rss/resolver.go internal/rss/resolver_social.go internal/rss/resolver_social_test.go
go test -count=1 ./internal/rss -run 'TestResolveFeedURL(Core|VideoPlatforms|SocialPlatforms)$'
```

Expected: PASS for all core, video, and social URL cases.

- [ ] **Step 5: Commit the social routing slice**

```bash
git add backend/internal/rss/resolver.go backend/internal/rss/resolver_social.go backend/internal/rss/resolver_social_test.go
git commit -m "feat: resolve mainstream social feed URLs"
```

### Task 3: CSDN, GitHub, and Fetch integration

**Files:**
- Modify: `backend/internal/rss/resolver.go`
- Create: `backend/internal/rss/resolver_content.go`
- Create: `backend/internal/rss/resolver_content_test.go`
- Create: `backend/internal/rss/resolver_fetcher_test.go`

- [ ] **Step 1: Write failing content-route and Fetch integration tests**

Create `backend/internal/rss/resolver_content_test.go`:

```go
package rss

import "testing"

func TestResolveFeedURLContentPlatforms(t *testing.T) {
	const base = "http://rsshub:1200"
	runResolveCases(t, []resolveCase{
		{name: "csdn_blog", input: "https://blog.csdn.net/csdngeeknews", rsshubBase: base, want: base + "/csdn/blog/csdngeeknews"},
		{name: "csdn_article_to_author", input: "https://blog.csdn.net/csdngeeknews/article/details/123?spm=1001", rsshubBase: base, want: base + "/csdn/blog/csdngeeknews"},
		{name: "csdn_reserved_nav", input: "https://blog.csdn.net/nav", rsshubBase: base, want: "https://blog.csdn.net/nav"},
		{name: "csdn_root", input: "https://blog.csdn.net/", rsshubBase: base, want: "https://blog.csdn.net/"},

		{name: "github_user", input: "https://github.com/DIYgod", rsshubBase: base, want: base + "/github/activity/DIYgod"},
		{name: "github_repo", input: "https://github.com/DIYgod/RSSHub", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_repo_git_suffix", input: "https://github.com/DIYgod/RSSHub.git", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_repo_subpage", input: "https://github.com/DIYgod/RSSHub/issues/123", rsshubBase: base, want: base + "/github/repo_event/DIYgod/RSSHub"},
		{name: "github_reserved_settings", input: "https://github.com/settings", rsshubBase: base, want: "https://github.com/settings"},
		{name: "github_reserved_orgs", input: "https://github.com/orgs/openai", rsshubBase: base, want: "https://github.com/orgs/openai"},
		{name: "github_root", input: "https://github.com/", rsshubBase: base, want: "https://github.com/"},
	})
}
```

Create `backend/internal/rss/resolver_fetcher_test.go`:

```go
package rss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetcherFetchUsesResolvedFeedURL(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var requestedURL string
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rss version="2.0"><channel><title>CSDN</title>` +
				`<link>https://blog.csdn.net/csdngeeknews</link><description>feed</description>` +
				`<item><title>post</title><link>https://example.com/post</link><guid>post</guid></item>` +
				`</channel></rss>`)),
		}, nil
	})}

	result, err := fetcher.Fetch(context.Background(), "https://blog.csdn.net/csdngeeknews", "", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if requestedURL != "http://rsshub.test:1200/csdn/blog/csdngeeknews" {
		t.Fatalf("requested URL = %q", requestedURL)
	}
	if result == nil || result.Feed == nil || result.Feed.Title != "CSDN" {
		t.Fatalf("unexpected fetch result: %#v", result)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd backend
go test -count=1 ./internal/rss -run 'TestResolveFeedURLContentPlatforms|TestFetcherFetchUsesResolvedFeedURL'
```

Expected: FAIL on `csdn_blog`; the Fetch integration test must report that the requested URL is still `https://blog.csdn.net/csdngeeknews` rather than the fake RSSHub URL.

- [ ] **Step 3: Implement content resolvers and register them**

Create `backend/internal/rss/resolver_content.go`:

```go
package rss

import (
	"net/url"
	"strings"
)

var csdnReservedNames = map[string]struct{}{
	"article": {},
	"community": {},
	"download": {},
	"nav": {},
	"rank": {},
}

var githubReservedNames = map[string]struct{}{
	"about": {},
	"account": {},
	"apps": {},
	"collections": {},
	"customer-stories": {},
	"enterprise": {},
	"events": {},
	"explore": {},
	"features": {},
	"issues": {},
	"login": {},
	"marketplace": {},
	"new": {},
	"notifications": {},
	"organizations": {},
	"orgs": {},
	"pricing": {},
	"pulls": {},
	"search": {},
	"security": {},
	"settings": {},
	"site": {},
	"sponsors": {},
	"topics": {},
	"trending": {},
	"users": {},
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
```

Update the resolver list in `backend/internal/rss/resolver.go` to exactly:

```go
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
```

- [ ] **Step 4: Format and verify GREEN**

Run:

```bash
gofmt -w internal/rss/resolver.go internal/rss/resolver_content.go internal/rss/resolver_content_test.go internal/rss/resolver_fetcher_test.go
go test -count=1 ./internal/rss -run 'TestResolveFeedURL|TestFetcherFetchUsesResolvedFeedURL'
```

Expected: PASS for all resolver cases, and the integration test must observe `http://rsshub.test:1200/csdn/blog/csdngeeknews`.

- [ ] **Step 5: Commit the content routing slice**

```bash
git add backend/internal/rss/resolver.go backend/internal/rss/resolver_content.go backend/internal/rss/resolver_content_test.go backend/internal/rss/resolver_fetcher_test.go
git commit -m "feat: resolve mainstream content feed URLs"
```

### Task 4: Full regression verification

**Files:**
- Verify: `backend/internal/rss/resolver.go`
- Verify: `backend/internal/rss/resolver_video.go`
- Verify: `backend/internal/rss/resolver_social.go`
- Verify: `backend/internal/rss/resolver_content.go`
- Verify: `backend/internal/rss/resolver_test.go`
- Verify: `backend/internal/rss/resolver_video_test.go`
- Verify: `backend/internal/rss/resolver_social_test.go`
- Verify: `backend/internal/rss/resolver_content_test.go`
- Verify: `backend/internal/rss/resolver_fetcher_test.go`

- [ ] **Step 1: Run the complete RSS package tests without cache**

```bash
cd backend
go test -count=1 ./internal/rss
```

Expected: PASS with `ok github.com/bytedance/rss-pal/internal/rss`.

- [ ] **Step 2: Run the complete backend test suite without cache**

```bash
go test -count=1 ./...
```

Expected: every package either reports `ok` or `[no test files]`; zero FAIL lines.

- [ ] **Step 3: Run static and diff checks**

```bash
go vet ./...
cd ..
git diff --check master...HEAD
git status --short --branch
```

Expected: `go vet` and `git diff --check` exit 0. Git status contains no untracked or modified implementation files; the branch is ahead of `master` only by the design, plan, and feature commits.

- [ ] **Step 4: Inspect the final diff against the approved design**

```bash
git diff --stat master...HEAD
git diff master...HEAD -- backend/internal/rss/resolver.go backend/internal/rss/resolver_video.go backend/internal/rss/resolver_social.go backend/internal/rss/resolver_content.go
```

Expected: only deterministic resolver and test changes, plus the approved spec/plan documents; no API, frontend, migration, Compose, or persistence changes.
