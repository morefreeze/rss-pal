package transcript

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
)

// stubDocFetcher is a tiny stub of the docFetcher interface
// HTMLPageScraper depends on. The real impl wraps ContentFetcher.
type stubDocFetcher struct {
	pages map[string]string
}

func (s *stubDocFetcher) FetchHTMLDocument(ctx context.Context, url string) (*goquery.Document, error) {
	html, ok := s.pages[url]
	if !ok {
		return nil, nil
	}
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

func TestHTMLPageScraper_InlineTranscript(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_inline_transcript.html"))
	stub := &stubDocFetcher{pages: map[string]string{"http://example.com/ep1": string(html)}}
	f := &HTMLPageScraper{Docs: stub}

	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/ep1",
		MediaType: "audio/mpeg",
	})
	if err != nil || got == nil {
		t.Fatalf("expected Result, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "first paragraph of the actual transcript") {
		t.Errorf("inline transcript missing expected text: %q", got.Text)
	}
}

func TestHTMLPageScraper_LinkedVTT(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_linked_vtt.html"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ep-42.vtt") {
			w.Write([]byte("WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nLinked transcript content."))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pageURL := "http://example.com/show/42"
	htmlWithBase := strings.ReplaceAll(string(html), `href="/transcripts/ep-42.vtt"`,
		`href="`+srv.URL+`/transcripts/ep-42.vtt"`)
	stub := &stubDocFetcher{pages: map[string]string{pageURL: htmlWithBase}}

	f := &HTMLPageScraper{Docs: stub, HTTPClient: srv.Client()}

	got, err := f.Fetch(context.Background(), &model.Article{URL: pageURL, MediaType: "audio/mpeg"})
	if err != nil || got == nil {
		t.Fatalf("expected Result, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "Linked transcript content") {
		t.Errorf("linked vtt content missing: %q", got.Text)
	}
}

func TestHTMLPageScraper_TwoHop(t *testing.T) {
	announce, _ := os.ReadFile(filepath.Join("testdata", "page_two_hop_announce.html"))
	target, _ := os.ReadFile(filepath.Join("testdata", "page_two_hop_target.html"))
	stub := &stubDocFetcher{pages: map[string]string{
		"http://example.com/programmes/ep1":        string(announce),
		"https://learn.example.com/episodes/ep-42": string(target),
	}}
	f := &HTMLPageScraper{Docs: stub}
	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/programmes/ep1",
		MediaType: "audio/mpeg",
	})
	if err != nil || got == nil {
		t.Fatalf("expected Result via two-hop, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "linked page after a two-hop traversal") {
		t.Errorf("two-hop transcript missing expected text: %q", got.Text)
	}
}

func TestHTMLPageScraper_NoTranscript(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_no_transcript.html"))
	stub := &stubDocFetcher{pages: map[string]string{"http://example.com/x": string(html)}}
	f := &HTMLPageScraper{Docs: stub}
	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/x",
		MediaType: "audio/mpeg",
	})
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil), got (%+v, %v)", got, err)
	}
}
