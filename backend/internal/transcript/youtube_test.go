package transcript

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestYouTubeCC_Skips_NonYouTube(t *testing.T) {
	f := &YouTubeCC{}
	got, err := f.Fetch(context.Background(), &model.Article{MediaType: "video/bilibili"})
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for non-youtube article, got (%+v, %v)", got, err)
	}
}

func TestYouTubeCC_FetchesTranscript(t *testing.T) {
	htmlBytes, err := os.ReadFile(filepath.Join("testdata", "youtube_watch_with_cc.html"))
	if err != nil {
		t.Fatal(err)
	}
	trackBytes, err := os.ReadFile(filepath.Join("testdata", "youtube_track.json3"))
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/watch"):
			html := strings.ReplaceAll(string(htmlBytes), "http://CAPTION_SERVER", r.Host)
			// reconstruct a usable absolute URL — easier to just use r.Host with scheme
			html = strings.ReplaceAll(string(htmlBytes), "http://CAPTION_SERVER", "http://"+r.Host)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
		case strings.Contains(r.URL.Path, "/api/timedtext"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(trackBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := &YouTubeCC{
		WatchURLBase: srv.URL + "/watch?v=",
	}

	// MediaURL is the embed URL (youtube-nocookie.com/embed/ID), but the
	// strategy reads ID from there for fetching the watch page.
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		MediaURL:  "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected Result, got nil")
	}
	if !strings.Contains(got.Text, "Hello world.") {
		t.Errorf("transcript text missing expected snippet: %q", got.Text)
	}
	if !strings.Contains(got.Text, "This is a test.") {
		t.Errorf("transcript text missing second event: %q", got.Text)
	}
	if got.Source == "" {
		t.Error("Source should not be empty")
	}
}

func TestYouTubeCC_NoCaptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body><p>nothing here</p></body></html>"))
	}))
	defer srv.Close()

	f := &YouTubeCC{WatchURLBase: srv.URL + "/watch?v="}
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		MediaURL:  "https://www.youtube-nocookie.com/embed/abcdEFGHijk?rel=0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil Result for video without captions, got %+v", got)
	}
}
