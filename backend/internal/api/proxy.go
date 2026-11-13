package api

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	proxyMaxBytes = 10 * 1024 * 1024 // 10MB
	// A complete multi-MB image can take over 30s through the configured
	// egress proxy. Keep a bounded full-body budget below nginx's image-only
	// read timeout; partial bodies must still never receive success headers.
	imageProxyTimeout  = 90 * time.Second
	mediaProxyMaxBytes = 512 * 1024 * 1024 // 512MB
	mediaProxyTimeout  = 10 * time.Minute
)

// ImageProxy serves remote images through this server. Constructed via
// NewImageProxy for production use; tests instantiate the struct directly
// with custom dependencies.
type ImageProxy struct {
	Validate func(rawURL string) (*url.URL, error)
	Client   *http.Client
}

// MediaProxy streams remote audio/video through this server. In addition to
// keeping browser playback behind the SSRF guard, it supplies a source-origin
// Referer for media CDNs that reject cross-site hotlinks.
type MediaProxy struct {
	Validate     func(rawURL string) (*url.URL, error)
	Client       *http.Client
	directClient *http.Client
}

// NewImageProxy returns a production-ready proxy: strict SSRF validation,
// 90s timeout, 10MB cap, and redirect re-validation against the SSRF guard.
func NewImageProxy() *ImageProxy {
	return &ImageProxy{
		Validate: httpx.ValidateURL,
		Client:   httpx.NewBufferedImageGETClient(imageProxyTimeout),
	}
}

func NewMediaProxy() *MediaProxy {
	directClient := httpx.NewDirectClient(mediaProxyTimeout)
	validateRedirect := directClient.CheckRedirect
	directClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !isXiaoyuzhouMediaURL(req.URL) {
			// Let the normal proxy handle redirects outside the direct route.
			return http.ErrUseLastResponse
		}
		return validateRedirect(req, via)
	}
	return &MediaProxy{
		Validate:     httpx.ValidateURL,
		Client:       httpx.NewClient(mediaProxyTimeout),
		directClient: directClient,
	}
}

// Handle is the gin handler.
func (p *ImageProxy) Handle(c *gin.Context) {
	raw := c.Query("url")
	target, err := p.Validate(raw)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid url: %s", err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), imageProxyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.String(http.StatusBadRequest, "build request: %s", err)
		return
	}
	// Inject a Referer matching the target origin to defeat hotlink protection
	// (notably WeChat / Zhihu).
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	req.Header.Set("User-Agent", httpx.UserAgent)

	resp, err := p.Client.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "upstream: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.String(http.StatusBadGateway, "upstream status %d", resp.StatusCode)
		return
	}

	// Content-Length precheck: reject if upstream declares oversize body.
	if cl := resp.ContentLength; cl > proxyMaxBytes {
		c.String(http.StatusBadGateway, "upstream too large: %d bytes", cl)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		c.String(http.StatusUnsupportedMediaType, "non-image content-type: %s", ct)
		return
	}

	// Do not send success/cache headers until the bounded upstream body has
	// completed. Otherwise an interrupted image can be cached for a week.
	body, err := io.ReadAll(io.LimitReader(resp.Body, proxyMaxBytes+1))
	if err != nil {
		c.String(http.StatusBadGateway, "read upstream image: %s", err)
		return
	}
	if len(body) > proxyMaxBytes {
		c.String(http.StatusBadGateway, "upstream image too large")
		return
	}

	c.Header("Content-Type", ct)
	if et := resp.Header.Get("ETag"); et != "" {
		c.Header("ETag", et)
	}
	c.Header("Cache-Control", "public, max-age=604800, immutable")
	c.Data(http.StatusOK, ct, body)
}

// Handle serves media GET/HEAD requests and preserves byte-range semantics so
// browsers can seek without downloading the whole file again.
func (p *MediaProxy) Handle(c *gin.Context) {
	raw := c.Query("url")
	raw, lennyEpisode := lennyPodcastUpstream(raw, c.Query("source"))
	target, err := p.Validate(raw)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid url: %s", err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), mediaProxyTimeout)
	defer cancel()

	method := http.MethodGet
	if c.Request.Method == http.MethodHead {
		method = http.MethodHead
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		c.String(http.StatusBadRequest, "build request: %s", err)
		return
	}
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	req.Header.Set("User-Agent", httpx.UserAgent)
	for _, name := range []string{"Range", "If-Range"} {
		if value := c.GetHeader(name); value != "" {
			req.Header.Set(name, value)
		}
	}

	resp, err := p.fetchMedia(req)
	if err != nil {
		c.String(http.StatusBadGateway, "upstream: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		c.String(http.StatusBadGateway, "upstream status %d", resp.StatusCode)
		return
	}
	if cl := resp.ContentLength; cl > mediaProxyMaxBytes {
		c.String(http.StatusBadGateway, "upstream too large: %d bytes", cl)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if lennyEpisode != "" && isSubstackPodcastMP3(resp, lennyEpisode) {
		ct = "audio/mpeg"
	}
	if !strings.HasPrefix(ct, "audio/") && !strings.HasPrefix(ct, "video/") {
		c.String(http.StatusUnsupportedMediaType, "non-media content-type: %s", ct)
		return
	}

	for _, name := range []string{
		"Content-Length", "Content-Range", "Accept-Ranges",
		"ETag", "Last-Modified",
	} {
		if value := resp.Header.Get(name); value != "" {
			c.Header(name, value)
		}
	}
	c.Header("Content-Type", ct)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=86400")
	c.Status(resp.StatusCode)
	if method == http.MethodHead {
		return
	}
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, mediaProxyMaxBytes))
}

func (p *MediaProxy) fetchMedia(req *http.Request) (*http.Response, error) {
	// Xiaoyuzhou's audio CDN is reachable directly from the deployment,
	// including when the configured outbound proxy stalls before headers.
	if p.directClient != nil && isXiaoyuzhouMediaURL(req.URL) {
		resp, err := p.directClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
				return resp, nil
			}
			resp.Body.Close()
		}
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
	}
	return p.Client.Do(req)
}

func isXiaoyuzhouMediaURL(target *url.URL) bool {
	return target.Scheme == "https" && strings.EqualFold(target.Host, "media.xyzcdn.net")
}

const lennyPodcastHost = "www.lennysnewsletter.com"

var lennyEnclosurePath = regexp.MustCompile(`^/feed/podcast/([0-9]+)/[a-f0-9]{32}\.mp3$`)
var substackMP3Path = regexp.MustCompile(`^/video_upload/post/([0-9]+)/[a-f0-9-]+/transcoded\.mp3$`)

// Lenny's publisher origin serves the same signed podcast redirect as the
// Substack enclosure, and is reachable from the production media relay.
func lennyPodcastUpstream(raw, source string) (string, string) {
	origin, err := url.Parse(source)
	if err != nil || origin.Scheme != "https" || origin.User != nil || !strings.EqualFold(origin.Host, lennyPodcastHost) || !strings.HasPrefix(origin.Path, "/p/") {
		return raw, ""
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.User != nil ||
		(!strings.EqualFold(target.Host, "api.substack.com") && !strings.EqualFold(target.Host, lennyPodcastHost)) {
		return raw, ""
	}
	match := lennyEnclosurePath.FindStringSubmatch(target.EscapedPath())
	if match == nil {
		return raw, ""
	}
	target.Host = lennyPodcastHost
	return target.String(), match[1]
}

func isSubstackPodcastMP3(resp *http.Response, episode string) bool {
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType != "binary/octet-stream" && contentType != "application/octet-stream" {
		return false
	}
	if resp.Request == nil || resp.Request.URL == nil {
		return false
	}
	final := resp.Request.URL
	if final.Scheme != "https" || !strings.EqualFold(final.Host, "substackcdn.com") {
		return false
	}
	match := substackMP3Path.FindStringSubmatch(final.EscapedPath())
	return match != nil && match[1] == episode
}
