package rss

import (
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

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
