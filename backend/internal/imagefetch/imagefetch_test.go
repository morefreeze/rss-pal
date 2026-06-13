package imagefetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// allowLoopback bypasses the SSRF guard's loopback rule so httptest servers
// (which always bind to 127.0.0.1) can be fetched in tests.
func allowLoopback(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

// solidPNG returns a w×h opaque-red PNG.
func solidPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	red := color.RGBA{R: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// solidJPEG returns a w×h opaque-red JPEG.
func solidJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	red := color.RGBA{R: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestFetchAndStore_basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(200, 100))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Dir:                   t.TempDir(),
		LocalArticleImagesDir: t.TempDir(),
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
		Validate:              allowLoopback,
	}
	srcURL := srv.URL + "/a.png"
	paths, urls, err := FetchAndStore(context.Background(), 42, []string{srcURL}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	if len(urls) != 1 || urls[0] != srcURL {
		t.Errorf("expected urls=[%q], got %v", srcURL, urls)
	}
	if !strings.HasSuffix(paths[0], "/42/0.jpg") {
		t.Errorf("expected path ending /42/0.jpg, got %s", paths[0])
	}
	st, err := os.Stat(paths[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() == 0 {
		t.Errorf("expected non-zero file")
	}
}

func TestFetchAndStore_resizesOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(3000, 1500))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Dir:         t.TempDir(),
		MaxLongSide: 1024,
		TTL:         24 * time.Hour,
		Validate:    allowLoopback,
	}
	paths, _, err := FetchAndStore(context.Background(), 7, []string{srv.URL + "/x.png"}, cfg)
	if err != nil || len(paths) != 1 {
		t.Fatalf("FetchAndStore: paths=%v err=%v", paths, err)
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > 1024 || b.Dy() > 1024 {
		t.Errorf("expected long side <= 1024, got %dx%d", b.Dx(), b.Dy())
	}
	if b.Dx() != 1024 {
		t.Errorf("expected width=1024 (3000 was longest), got %d", b.Dx())
	}
}

func TestFetchAndStore_cacheHitRefreshesMtime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(50, 50))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{Dir: t.TempDir(), MaxLongSide: 1024, TTL: 24 * time.Hour, Validate: allowLoopback}
	first, _, err := FetchAndStore(context.Background(), 1, []string{srv.URL + "/a.png"}, cfg)
	if err != nil || len(first) != 1 {
		t.Fatalf("first: paths=%v err=%v", first, err)
	}

	// Backdate mtime by 1 hour.
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(first[0], old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	second, _, err := FetchAndStore(context.Background(), 1, []string{srv.URL + "/a.png"}, cfg)
	if err != nil || len(second) != 1 || second[0] != first[0] {
		t.Fatalf("second: paths=%v err=%v", second, err)
	}
	st, err := os.Stat(second[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !st.ModTime().After(old) {
		t.Errorf("expected mtime refresh; mtime=%v old=%v", st.ModTime(), old)
	}
}

func TestFetchAndStore_localArticleImagesURL(t *testing.T) {
	// Pre-seed an on-disk PDF clip image as if from a previous extraction.
	localDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(localDir, "9"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(localDir, "9", "0.png")
	if err := os.WriteFile(target, solidPNG(20, 20), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Dir:                   t.TempDir(),
		LocalArticleImagesDir: localDir,
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
		Validate:              allowLoopback,
	}
	paths, _, err := FetchAndStore(context.Background(), 9, []string{"/api/articles/9/images/0.png"}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	if paths[0] != target {
		t.Errorf("want local path %s, got %s", target, paths[0])
	}
	// Verify the cache dir was NOT used.
	cacheEntries, _ := os.ReadDir(cfg.Dir)
	if len(cacheEntries) != 0 {
		t.Errorf("expected cache dir untouched, got entries: %v", cacheEntries)
	}
}

func TestFetchAndStore_skipsFailures(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(solidJPEG(40, 40))
	}))
	t.Cleanup(good.Close)

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	corrupt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not actually a png"))
	}))
	t.Cleanup(corrupt.Close)

	cfg := Config{Dir: t.TempDir(), MaxLongSide: 1024, TTL: 24 * time.Hour, Validate: allowLoopback}
	goodA := good.URL + "/a.jpg"
	goodD := good.URL + "/d.jpg"
	paths, urls, err := FetchAndStore(context.Background(), 5, []string{
		goodA,
		bad.URL + "/b.jpg",
		corrupt.URL + "/c.png",
		goodD,
	}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 successful paths (indices 0 and 3 of good server), got %d: %v", len(paths), paths)
	}
	if len(urls) != 2 || urls[0] != goodA || urls[1] != goodD {
		t.Errorf("expected gotURLs to mirror successful inputs [%q, %q], got %v", goodA, goodD, urls)
	}
}

func TestCleanupExpired(t *testing.T) {
	cacheDir := t.TempDir()
	localDir := t.TempDir()

	// Seed cache files at varied ages.
	fresh := filepath.Join(cacheDir, "10", "0.jpg")
	old := filepath.Join(cacheDir, "11", "0.jpg")
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate `old` to 25 hours ago.
	backdate := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, backdate, backdate); err != nil {
		t.Fatal(err)
	}

	// Seed a file in LocalArticleImagesDir that's even older — must NOT be touched.
	localImg := filepath.Join(localDir, "99", "0.jpg")
	if err := os.MkdirAll(filepath.Dir(localImg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localImg, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	ancient := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(localImg, ancient, ancient); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Dir:                   cacheDir,
		LocalArticleImagesDir: localDir,
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
	}
	removed, err := CleanupExpired(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if removed != 1 {
		t.Errorf("want removed=1, got %d", removed)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should still exist: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Errorf("empty article-id subdir should be removed, got err=%v", err)
	}
	if _, err := os.Stat(localImg); err != nil {
		t.Errorf("local file must not be touched: %v", err)
	}
}
