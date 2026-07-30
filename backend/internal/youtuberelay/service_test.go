package youtuberelay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	mu      sync.Mutex
	results []ResolvedMedia
	err     error
	calls   int
}

func (r *fakeResolver) Resolve(_ context.Context, _ string) (ResolvedMedia, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return ResolvedMedia{}, r.err
	}
	index := r.calls - 1
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	return r.results[index], nil
}

func testResolvedMedia(baseURL string) ResolvedMedia {
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 212, Formats: []Format{
		{ID: "137", URL: baseURL + "/video", Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Width: 1920, Height: 1080, FPS: 30, TBR: 3500},
		{ID: "140", URL: baseURL + "/audio", Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ASR: 44100, ABR: 128},
		{ID: "22", URL: baseURL + "/progressive", Ext: "mp4", VCodec: "avc1.64001f", ACodec: "mp4a.40.2", Width: 1280, Height: 720, FPS: 30, TBR: 2400},
	}}
	return ResolvedMedia{
		Info: info,
		Selection: Selection{
			Video:       &info.Formats[0],
			Audio:       &info.Formats[1],
			Progressive: &info.Formats[2],
			Quality:     1080,
		},
	}
}

func indexedMP4Prefix() []byte {
	out := append(mp4Box("ftyp", make([]byte, 16)), mp4Box("moov", make([]byte, 72))...)
	out = append(out, mp4Box("sidx", make([]byte, 40))...)
	out = append(out, mp4Box("moof", make([]byte, 24))...)
	return out
}

func TestServiceStartsDASHSessionWithoutCompleteDownload(t *testing.T) {
	var (
		mu     sync.Mutex
		ranges []string
	)
	prefix := indexedMP4Prefix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(prefix)
	}))
	defer server.Close()

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	resolver := &fakeResolver{results: []ResolvedMedia{testResolvedMedia(server.URL)}}
	service := NewService(ServiceOptions{
		Resolver:        resolver,
		Client:          server.Client(),
		MaxSessions:     2,
		Now:             func() time.Time { return now },
		UpstreamAllowed: func(raw string) bool { return strings.HasPrefix(raw, server.URL+"/") },
	})
	defer service.Close()

	playback, err := service.Start(context.Background(), StartRequest{
		UserID: 7, ArticleID: 2391, VideoID: "dQw4w9WgXcQ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if playback.Mode != "dash" || playback.Quality != 1080 || !playback.HasProgressive {
		t.Fatalf("unexpected playback: %+v", playback)
	}
	if len(playback.Ticket) != 43 {
		t.Fatalf("ticket length = %d, want 43", len(playback.Ticket))
	}
	manifest, err := service.Manifest(playback.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), server.URL) || !strings.Contains(string(manifest), playback.Ticket) {
		t.Fatalf("unsafe or missing ticket MPD: %s", manifest)
	}

	mu.Lock()
	gotRanges := append([]string(nil), ranges...)
	mu.Unlock()
	if len(gotRanges) != 2 {
		t.Fatalf("probe requests = %d, want video+audio", len(gotRanges))
	}
	for _, got := range gotRanges {
		if got != "bytes=0-1048575" {
			t.Fatalf("probe Range = %q, want bounded first MiB", got)
		}
	}

	reused, err := service.Start(context.Background(), StartRequest{
		UserID: 7, ArticleID: 2391, VideoID: "dQw4w9WgXcQ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reused.Ticket != playback.Ticket || resolver.calls != 1 {
		t.Fatalf("session not reused: first=%s reused=%s resolver_calls=%d", playback.Ticket, reused.Ticket, resolver.calls)
	}
}

func TestServiceForwardsSingleRangeAndRejectsMultipleRanges(t *testing.T) {
	prefix := indexedMP4Prefix()
	var playbackRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=10-19" {
			playbackRange = r.Header.Get("Range")
			w.Header().Set("Content-Range", "bytes 10-19/100")
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("0123456789"))
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(prefix)
	}))
	defer server.Close()

	service := NewService(ServiceOptions{
		Resolver:        &fakeResolver{results: []ResolvedMedia{testResolvedMedia(server.URL)}},
		Client:          server.Client(),
		MaxSessions:     2,
		UpstreamAllowed: func(raw string) bool { return strings.HasPrefix(raw, server.URL+"/") },
	})
	defer service.Close()
	playback, err := service.Start(context.Background(), StartRequest{UserID: 1, ArticleID: 2, VideoID: "dQw4w9WgXcQ"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := service.Open(context.Background(), http.MethodGet, playback.Ticket, StreamVideo, "bytes=10-19", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "0123456789" || playbackRange != "bytes=10-19" {
		t.Fatalf("unexpected relay response status=%d body=%q range=%q", resp.StatusCode, body, playbackRange)
	}

	if _, err := service.Open(context.Background(), http.MethodGet, playback.Ticket, StreamVideo, "bytes=0-1,3-4", ""); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("error = %v, want ErrInvalidRange", err)
	}
}

func TestServiceEnforcesCapacityAndIdleExpiry(t *testing.T) {
	prefix := indexedMP4Prefix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(prefix)
	}))
	defer server.Close()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	resolver := &fakeResolver{results: []ResolvedMedia{
		testResolvedMedia(server.URL),
		testResolvedMedia(server.URL),
	}}
	service := NewService(ServiceOptions{
		Resolver:        resolver,
		Client:          server.Client(),
		MaxSessions:     1,
		IdleTTL:         10 * time.Minute,
		Now:             func() time.Time { return now },
		UpstreamAllowed: func(raw string) bool { return strings.HasPrefix(raw, server.URL+"/") },
	})
	defer service.Close()

	first, err := service.Start(context.Background(), StartRequest{UserID: 1, ArticleID: 1, VideoID: "dQw4w9WgXcQ"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), StartRequest{UserID: 2, ArticleID: 2, VideoID: "dQw4w9WgXcQ"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("error = %v, want ErrCapacity", err)
	}

	now = now.Add(11 * time.Minute)
	if _, err := service.Start(context.Background(), StartRequest{UserID: 2, ArticleID: 2, VideoID: "dQw4w9WgXcQ"}); err != nil {
		t.Fatalf("start after idle expiry: %v", err)
	}
	if _, err := service.Manifest(first.Ticket); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("old manifest error = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceRefreshesExpiredUpstreamOnce(t *testing.T) {
	prefix := indexedMP4Prefix()
	var freshRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/expired") && r.Header.Get("Range") == "bytes=25-35" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/fresh") && r.Header.Get("Range") == "bytes=25-35" {
			freshRequests++
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("fresh"))
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(prefix)
	}))
	defer server.Close()

	first := testResolvedMedia(server.URL)
	first.Selection.Video.URL = server.URL + "/expired"
	first.Info.Formats[0].URL = server.URL + "/expired"
	second := testResolvedMedia(server.URL)
	second.Selection.Video.URL = server.URL + "/fresh"
	second.Info.Formats[0].URL = server.URL + "/fresh"
	resolver := &fakeResolver{results: []ResolvedMedia{first, second}}
	service := NewService(ServiceOptions{
		Resolver:        resolver,
		Client:          server.Client(),
		MaxSessions:     2,
		UpstreamAllowed: func(raw string) bool { return strings.HasPrefix(raw, server.URL+"/") },
	})
	defer service.Close()

	playback, err := service.Start(context.Background(), StartRequest{UserID: 1, ArticleID: 1, VideoID: "dQw4w9WgXcQ"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := service.Open(context.Background(), http.MethodGet, playback.Ticket, StreamVideo, "bytes=25-35", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resolver.calls != 2 || freshRequests != 1 {
		t.Fatalf("status=%d resolver_calls=%d fresh_requests=%d", resp.StatusCode, resolver.calls, freshRequests)
	}
}
