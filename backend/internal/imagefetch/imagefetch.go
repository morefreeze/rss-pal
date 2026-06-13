// Package imagefetch downloads article images for the AI vision summary path
// to a local TTL-cleaned cache. The downloaded files live under
// cfg.Dir/<articleID>/<idx>.jpg (always normalised to JPEG) and are reused
// across repeated summarize calls within cfg.TTL.
//
// Images already on local disk (those whose URL points to the rss-pal
// article-images endpoint, served by api/article_images.go) are resolved to
// their pdfextract-managed location and returned directly — never copied or
// modified, never affected by CleanupExpired.
package imagefetch

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register GIF decoder
	"image/jpeg"
	_ "image/png" // register PNG decoder
	_ "golang.org/x/image/webp" // register WebP decoder (WeChat wx_fmt=other is WebP)
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	downloadTimeout = 30 * time.Second
	maxDownload     = 10 * 1024 * 1024 // 10 MB pre-decode cap; matches proxy
)

// Config holds all knobs FetchAndStore + CleanupExpired need. Construct
// from config.VisionConfig at the call site.
type Config struct {
	Dir                   string        // AI summary image cache root
	LocalArticleImagesDir string        // where PDF clip images live (read-only from this pkg's POV)
	MaxLongSide           int           // resize trigger; px
	TTL                   time.Duration // cache file age limit

	// Validate optionally overrides the URL validator. Defaults to
	// httpx.ValidateURL. Tests inject an allow-loopback variant so they can
	// fetch from httptest.NewServer without tripping the SSRF guard.
	Validate func(raw string) (*url.URL, error)
}

var localArticleImageRE = regexp.MustCompile(`^/api/articles/(\d+)/images/(\d+)\.([a-z0-9]+)$`)

// FetchAndStore implements the spec contract. See package doc.
//
// Returns two parallel slices: paths[i] is the on-disk JPEG for the URL
// gotURLs[i]. Failed fetches are skipped from BOTH slices so the alignment is
// preserved — callers can rely on paths[i] ↔ gotURLs[i] when threading the
// original URL into downstream prompts (e.g. for the vision summary to insert
// markdown image references).
func FetchAndStore(ctx context.Context, articleID int, urls []string, cfg Config) (paths []string, gotURLs []string, err error) {
	if cfg.Dir == "" {
		return nil, nil, errors.New("imagefetch: Config.Dir is required")
	}
	if cfg.MaxLongSide <= 0 {
		cfg.MaxLongSide = 1024
	}

	client := httpx.NewClient(downloadTimeout)
	paths = make([]string, 0, len(urls))
	gotURLs = make([]string, 0, len(urls))
	for idx, raw := range urls {
		path, ferr := fetchOne(ctx, client, articleID, idx, raw, cfg)
		if ferr != nil {
			log.Printf("imagefetch: article %d idx %d %q: %v", articleID, idx, raw, ferr)
			continue
		}
		paths = append(paths, path)
		gotURLs = append(gotURLs, raw)
	}
	return paths, gotURLs, nil
}

func fetchOne(ctx context.Context, client *http.Client, articleID, idx int, raw string, cfg Config) (string, error) {
	// Local article-images URL → resolve to disk, no copy.
	if m := localArticleImageRE.FindStringSubmatch(raw); m != nil {
		if cfg.LocalArticleImagesDir == "" {
			return "", errors.New("local article-images URL but LocalArticleImagesDir not configured")
		}
		// m[1] = source article id (may differ from articleID if quoted from elsewhere — use the URL's, not articleID).
		path := filepath.Join(cfg.LocalArticleImagesDir, m[1], m[2]+"."+m[3])
		st, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("local image missing: %w", err)
		}
		if st.Size() == 0 {
			return "", errors.New("local image empty")
		}
		return path, nil
	}

	// Remote URL — go through SSRF guard, cache.
	validate := cfg.Validate
	if validate == nil {
		validate = httpx.ValidateURL
	}
	if _, err := validate(raw); err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	dir := filepath.Join(cfg.Dir, strconv.Itoa(articleID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	dest := filepath.Join(dir, strconv.Itoa(idx)+".jpg")

	// Cache hit: refresh mtime, return.
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(dest, now, now)
		return dest, nil
	}

	// Download.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	// Spoof a Referer matching origin so hotlink-protected CDNs (WeChat, Zhihu) don't 403.
	if i := strings.Index(raw, "://"); i > 0 {
		if j := strings.Index(raw[i+3:], "/"); j > 0 {
			req.Header.Set("Referer", raw[:i+3+j]+"/")
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxDownload {
		return "", fmt.Errorf("upstream too large: > %d bytes", maxDownload)
	}

	// Decode (PNG/JPEG/GIF — see blank imports above).
	img, _, err := image.Decode(byteReader(body))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	// Resize if over budget.
	img = resizeIfNeeded(img, cfg.MaxLongSide)

	// Encode as JPEG q85, atomic write.
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create tmp: %w", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("encode: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename: %w", err)
	}
	return dest, nil
}

// resizeIfNeeded scales img down so max(W,H) ≤ maxLongSide, preserving aspect.
// Uses BiLinear for a reasonable speed/quality tradeoff; CatmullRom would be
// sharper but ~3-4x slower.
func resizeIfNeeded(src image.Image, maxLongSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxLongSide {
		return src
	}
	ratio := float64(maxLongSide) / float64(long)
	nw := int(float64(w) * ratio)
	nh := int(float64(h) * ratio)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// byteReader wraps a []byte in an io.Reader without adopting bytes.Buffer's
// extra allocation cost.
func byteReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b   []byte
	pos int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}

// CleanupExpired walks cfg.Dir and removes every regular file whose mtime is
// older than cfg.TTL. After processing each <articleID> subdir, removes the
// subdir if it is now empty. Errors per-file are logged + counted; the walk
// is not aborted on individual failures. Returns the number of files
// successfully removed.
//
// cfg.LocalArticleImagesDir is never visited.
func CleanupExpired(ctx context.Context, cfg Config) (int, error) {
	if cfg.Dir == "" {
		return 0, errors.New("imagefetch: Config.Dir is required")
	}
	if cfg.TTL <= 0 {
		return 0, errors.New("imagefetch: Config.TTL must be positive")
	}
	threshold := time.Now().Add(-cfg.TTL)
	removed := 0

	rootEntries, err := os.ReadDir(cfg.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("readdir: %w", err)
	}

	for _, subEnt := range rootEntries {
		if !subEnt.IsDir() {
			continue
		}
		subPath := filepath.Join(cfg.Dir, subEnt.Name())
		fileEntries, err := os.ReadDir(subPath)
		if err != nil {
			log.Printf("imagefetch cleanup: readdir %s: %v", subPath, err)
			continue
		}
		remaining := 0
		for _, fEnt := range fileEntries {
			if fEnt.IsDir() {
				remaining++
				continue
			}
			fPath := filepath.Join(subPath, fEnt.Name())
			info, err := fEnt.Info()
			if err != nil {
				log.Printf("imagefetch cleanup: info %s: %v", fPath, err)
				remaining++
				continue
			}
			if info.ModTime().Before(threshold) {
				if err := os.Remove(fPath); err != nil {
					log.Printf("imagefetch cleanup: remove %s: %v", fPath, err)
					remaining++
					continue
				}
				removed++
			} else {
				remaining++
			}
		}
		if remaining == 0 {
			if err := os.Remove(subPath); err != nil {
				log.Printf("imagefetch cleanup: rmdir %s: %v", subPath, err)
			}
		}
		// Cooperative cancellation between subdirs.
		select {
		case <-ctx.Done():
			return removed, ctx.Err()
		default:
		}
	}
	return removed, nil
}
