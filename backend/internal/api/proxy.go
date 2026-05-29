package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	proxyMaxBytes = 10 * 1024 * 1024 // 10MB
	proxyTimeout  = 30 * time.Second
)

// ImageProxy serves remote images through this server. Constructed via
// NewImageProxy for production use; tests instantiate the struct directly
// with custom dependencies.
type ImageProxy struct {
	Validate func(rawURL string) (*url.URL, error)
	Client   *http.Client
}

// NewImageProxy returns a production-ready proxy: strict SSRF validation,
// 30s timeout, 10MB cap, and redirect re-validation against the SSRF guard.
func NewImageProxy() *ImageProxy {
	return &ImageProxy{
		Validate: httpx.ValidateURL,
		Client:   httpx.NewClient(proxyTimeout),
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), proxyTimeout)
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

	c.Header("Content-Type", ct)
	if et := resp.Header.Get("ETag"); et != "" {
		c.Header("ETag", et)
	}
	c.Header("Cache-Control", "public, max-age=604800, immutable")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, proxyMaxBytes))
}
