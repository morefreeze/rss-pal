// Package imgmeta probes remote image URLs for their intrinsic dimensions.
//
// Used by the backfill_image_dimensions binary (and, eventually, the scrape
// pipeline) so the frontend can render <img width=W height=H ...> and the
// browser reserves correct layout space for lazy-loaded images. Without
// width/height the document scrollHeight grows as images decode, which
// pushes reading-progress backwards.
package imgmeta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	// probeTimeout caps a single image fetch. Network-stricken hosts shouldn't
	// stall the worker; failures fall back to NULL dimensions.
	probeTimeout = 15 * time.Second
	// peekBytes is the upper bound on bytes pulled from the image stream.
	// image.DecodeConfig only needs the header — PNG/JPEG/GIF/WebP headers
	// fit comfortably under 64KiB even for progressive JPEGs.
	peekBytes = 64 * 1024
)

// Dimensions is a pair of intrinsic pixel sizes. Stored on articles as a
// JSON array [W, H] keyed by the original (pre-proxy) URL.
type Dimensions struct {
	Width  int `json:"w"`
	Height int `json:"h"`
}

// Prober probes image URLs. Tests construct it directly with a custom
// client + validator; production code uses New.
type Prober struct {
	Validate func(rawURL string) (*url.URL, error)
	Client   *http.Client
}

var defaultProber *Prober

// New returns a Prober wired with the shared SSRF guard + redirect-aware
// HTTP client used by the image proxy.
func New() *Prober {
	return &Prober{
		Validate: httpx.ValidateURL,
		Client:   httpx.NewClient(probeTimeout),
	}
}

// Probe returns the intrinsic width/height of the image at rawURL. The URL
// is SSRF-validated; non-image content-types, oversized declared bodies,
// and decode failures all return an error.
func (p *Prober) Probe(ctx context.Context, rawURL string) (Dimensions, error) {
	if p.Validate == nil || p.Client == nil {
		return Dimensions{}, errors.New("imgmeta: prober not initialised")
	}
	target, err := p.Validate(rawURL)
	if err != nil {
		return Dimensions{}, fmt.Errorf("validate: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Dimensions{}, fmt.Errorf("new request: %w", err)
	}
	// Match the proxy: Referer defeats hotlink protection on WeChat/Zhihu,
	// UA satisfies servers that reject empty/Go-default UA.
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	req.Header.Set("User-Agent", httpx.UserAgent)
	// Ask only for the header bytes. Servers that ignore Range still work
	// because we cap io.Read below.
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", peekBytes-1))

	resp, err := p.Client.Do(req)
	if err != nil {
		return Dimensions{}, fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()

	// 200 OK (server ignored Range) or 206 Partial Content both work.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return Dimensions{}, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return Dimensions{}, fmt.Errorf("non-image content-type: %s", ct)
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, io.LimitReader(resp.Body, peekBytes)); err != nil {
		return Dimensions{}, fmt.Errorf("read body: %w", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return Dimensions{}, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Dimensions{}, fmt.Errorf("invalid dimensions: %dx%d", cfg.Width, cfg.Height)
	}
	return Dimensions{Width: cfg.Width, Height: cfg.Height}, nil
}

// Probe is a convenience wrapper using the default prober. Safe for
// concurrent use.
func Probe(ctx context.Context, rawURL string) (Dimensions, error) {
	if defaultProber == nil {
		defaultProber = New()
	}
	return defaultProber.Probe(ctx, rawURL)
}
