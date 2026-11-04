package rss

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func TestFetchHTMLSkipsZeroPublicationDate(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"0001-01-01T00:00:00Z", ""},
		{"2026-09-20T08:30:00Z", "2026-09-20T08:30:00Z"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `<html><title>Date fixture</title><body><article><a href="/post/faq">FAQ date fixture</a><time datetime="%s"></time></article></body></html>`, tc.source)
			}))
			defer server.Close()
			feed, err := NewFetcher("").FetchHTML(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if len(feed.Items) != 1 {
				t.Fatalf("expected one HTML article, got %+v", feed.Items)
			}
			got := feed.Items[0].PublishedParsed
			if tc.want == "" {
				if got != nil {
					t.Fatalf("zero HTML publication date exposed to worker: %v", got)
				}
			} else if got == nil || got.Format(time.RFC3339) != tc.want {
				t.Fatalf("valid HTML publication date changed: %v", got)
			}
		})
	}
}

func TestRSSPreviewOmitsZeroPublicationDate(t *testing.T) {
	// Exact absent-date representation in https://lilianweng.github.io/index.xml.
	raw := `<rss version="2.0"><channel><title>Lil'Log</title><item><title>FAQ</title><link>https://lilianweng.github.io/faq/</link><pubDate>Mon, 01 Jan 0001 00:00:00 +0000</pubDate></item></channel></rss>`
	feed, err := gofeed.NewParser().Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	preview := rssToPreview(feed, "https://lilianweng.github.io/index.xml")
	if len(preview.Items) != 1 || preview.Items[0].PublishedAt != nil {
		t.Fatalf("absent date exposed by preview: %+v", preview.Items)
	}
}

func TestPublishedTimeStrSkipsUnknownDates(t *testing.T) {
	zero := time.Time{}
	valid := time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name               string
		published, updated *time.Time
		want               string
	}{
		{"missing", nil, nil, ""},
		{"zero", &zero, nil, ""},
		{"both zero", &zero, &zero, ""},
		{"valid published", &valid, &zero, "2026-09-20T08:30:00Z"},
		{"valid existing update fallback", &zero, &valid, "2026-09-20T08:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publishedTimeStr(tc.published, tc.updated); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
