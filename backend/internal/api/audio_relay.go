package api

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/rss-pal/internal/httpx"
)

// AudioMediaSource looks up the stored enclosure/media URL for an article
// without user scoping. Production wiring goes through a bypass-RLS pool
// (see cmd/server); tests provide a fake.
type AudioMediaSource interface {
	GetMediaByID(id int) (mediaURL, mediaType string, err error)
}

// AudioRelayHandler streams a stored article enclosure (podcast mp3/m4a)
// through this server so browser <audio> playback works even when the CDN
// hosting the file is unreachable from the client's network (e.g. Substack
// behind the GFW). Byte-range requests are forwarded verbatim so seeking
// works; responses stream end-to-end with no size cap — a long podcast must
// not be buffered or cut off by an overall client timeout.
type AudioRelayHandler struct {
	media  AudioMediaSource
	client *http.Client
}

// NewAudioRelayHandler builds the handler. proxyURL, when non-empty, is used
// for every upstream fetch (e.g. an OCI squid tunnel reachable only from the
// server). When empty the transport falls back to standard proxy env vars.
func NewAudioRelayHandler(media AudioMediaSource, proxyURL string) *AudioRelayHandler {
	proxy := http.ProxyFromEnvironment
	if raw := strings.TrimSpace(proxyURL); raw != "" {
		fixed, err := url.Parse(raw)
		if err == nil && fixed.Scheme != "" && fixed.Host != "" {
			proxy = func(req *http.Request) (*url.URL, error) { return fixed, nil }
		} else {
			log.Printf("audio_relay: ignoring invalid MEDIA_PROXY_URL %q: %v", raw, err)
		}
	}
	return &AudioRelayHandler{
		media: media,
		client: &http.Client{
			// No overall Timeout on purpose: it would cap body streaming and
			// kill hour-long podcasts. Per-phase timeouts live on the transport.
			Transport: &http.Transport{
				Proxy:                 proxy,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
			// Upstream redirects (e.g. api.substack.com → substackcdn.com)
			// must keep passing the SSRF guard.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errTooManyRedirects
				}
				if _, err := httpx.ValidateURL(req.URL.String()); err != nil {
					return err
				}
				return nil
			},
		},
	}
}

var errTooManyRedirects = &audioRelayError{"too many redirects"}

type audioRelayError struct{ msg string }

func (e *audioRelayError) Error() string { return e.msg }

// Serve handles GET/HEAD /api/media/audio/:id.
//
// Auth model: intentionally public, mirroring the PDF clip image route —
// browser media elements do not reliably attach the app's Authorization
// header, and for this personal single-user deployment the article id in the
// URL is an acceptable access token (the only thing behind it is an audio
// enclosure the RSS worker already fetched publicly).
func (h *AudioRelayHandler) Serve(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	mediaURL, _, err := h.media.GetMediaByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}
	if mediaURL == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "article has no media"})
		return
	}
	target, err := httpx.ValidateURL(mediaURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid media url"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "build request"})
		return
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	if v := c.GetHeader("Range"); v != "" {
		req.Header.Set("Range", v)
	}
	if v := c.GetHeader("If-Range"); v != "" {
		req.Header.Set("If-Range", v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("audio_relay id=%d upstream error: %v", id, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "audio upstream failed"})
		return
	}
	defer resp.Body.Close()

	// 416 (unsatisfiable range) is passed through so the browser can recover.
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable &&
		(resp.StatusCode < 200 || resp.StatusCode >= 300) {
		log.Printf("audio_relay id=%d upstream status %d", id, resp.StatusCode)
		c.JSON(http.StatusBadGateway, gin.H{"error": "audio upstream status", "status": resp.StatusCode})
		return
	}
	if !audioContentTypeAllowed(resp.Header.Get("Content-Type")) {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "non-audio content-type", "content_type": resp.Header.Get("Content-Type")})
		return
	}

	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"ETag",
		"Last-Modified",
	} {
		for _, value := range resp.Header.Values(name) {
			c.Writer.Header().Add(name, value)
		}
	}
	// Same policy as the YouTube relay: media is per-user content behind a
	// CDN we don't control, so keep intermediaries from caching it.
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(resp.StatusCode)
	if c.Request.Method == http.MethodHead {
		return
	}
	written, copyErr := io.CopyBuffer(c.Writer, resp.Body, make([]byte, 64<<10))
	if copyErr != nil {
		// Client disconnects mid-stream are routine (pause/seek); log at
		// info-level verbosity without the stack.
		log.Printf("audio_relay id=%d status=%d bytes=%d copy_error=%v", id, resp.StatusCode, written, copyErr)
		return
	}
	log.Printf("audio_relay id=%d status=%d bytes=%d", id, resp.StatusCode, written)
}

// audioContentTypeAllowed accepts anything an <audio> element can reasonably
// play. Podcast CDNs are loose with types (octet-stream for mp3, video/mp4
// for m4a), so the list is permissive but still blocks text/html — that
// would mean the "media URL" actually points at a page.
func audioContentTypeAllowed(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch {
	case strings.HasPrefix(ct, "audio/"):
		return true
	case ct == "video/mp4", ct == "video/webm", ct == "video/ogg",
		ct == "application/octet-stream", ct == "application/mpeg":
		return true
	default:
		return false
	}
}
