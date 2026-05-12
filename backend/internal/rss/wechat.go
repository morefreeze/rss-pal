package rss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// WeChat 公众号 subscription works by routing the user's mp.weixin.qq.com
// URL through RSSHub's `/wechat/ce/<biz>` endpoint, which produces a normal
// RSS feed. The only WeChat-specific work is extracting `__biz` (the base64
// public-account ID) from whatever URL the user pasted.

// Two patterns to pull __biz out of mp.weixin.qq.com article HTML. We try
// them in order and use whichever fires first:
//
//   bizVarPattern   — legacy `var biz = "MzI5..."` JS assignment. Still
//                     present on older article templates.
//   bizParamPattern — modern templates skip the JS variable but include
//                     `__biz=Mz...` inside a canonical URL or share link
//                     (e.g. `__biz=Mzk0NDczMjYwOA==&mid=...`). Stops at any
//                     character that isn't a valid base64/url-safe biz char.
var (
	bizVarPattern   = regexp.MustCompile(`var\s+biz\s*=\s*['"]([^'"]+)['"]`)
	bizParamPattern = regexp.MustCompile(`__biz=([A-Za-z0-9+/=%]{8,})`)
)

// IsWeChatURL reports whether the URL points to an mp.weixin.qq.com page.
// True for any host with that suffix; permits sub-prefixes like
// `https://mp.weixin.qq.com/...` (the only host WeChat actually serves
// article and profile pages on).
func IsWeChatURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "mp.weixin.qq.com" || strings.HasSuffix(host, ".mp.weixin.qq.com")
}

// extractBizFromQuery looks for `__biz=...` in the URL's query string and
// returns it (the value may be URL-encoded; url.Parse decodes it). Returns
// empty string when absent. Pure function — no network.
func extractBizFromQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("__biz"))
}

// ExtractBiz pulls the `__biz` public-account ID out of a WeChat URL. It
// first tries the query string (zero network for long-form share links), and
// only on miss does an HTTP GET to scrape `var biz = "..."` from the article
// HTML — required for short links like mp.weixin.qq.com/s/<hash>.
//
// Returns a wrapped error so handlers can surface a user-readable message.
func ExtractBiz(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if biz := extractBizFromQuery(rawURL); biz != "" {
		return biz, nil
	}
	if client == nil {
		return "", fmt.Errorf("wechat: __biz not in URL and no http client provided")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("wechat: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("wechat: fetch article: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wechat: article returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("wechat: read article body: %w", err)
	}
	biz := matchBiz(body)
	if biz == "" {
		return "", fmt.Errorf("wechat: __biz not found in article HTML")
	}
	return biz, nil
}

// matchBiz tries each known biz-extraction pattern against the HTML and
// returns the first non-empty hit. URL-encoded values (`Mz...%3D%3D`) are
// decoded so callers always get the raw base64 form.
func matchBiz(body []byte) string {
	for _, pat := range []*regexp.Regexp{bizVarPattern, bizParamPattern} {
		m := pat.FindSubmatch(body)
		if len(m) < 2 {
			continue
		}
		biz := strings.TrimSpace(string(m[1]))
		if biz == "" {
			continue
		}
		if decoded, err := url.QueryUnescape(biz); err == nil {
			biz = decoded
		}
		return biz
	}
	return ""
}

// BuildFeedURL composes the RSSHub `/wechat/ce/<biz>` URL that fronts a
// public-account subscription. Trailing slashes on rsshubBase are tolerated.
// Returns "" if either input is empty so callers can fail fast.
func BuildFeedURL(rsshubBase, biz string) string {
	if rsshubBase == "" || biz == "" {
		return ""
	}
	return strings.TrimRight(rsshubBase, "/") + "/wechat/ce/" + biz
}
