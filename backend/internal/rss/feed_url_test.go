package rss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResolveFeedURLUpgradesLegacyFeedBurnerWithoutRSSHub(t *testing.T) {
	for _, base := range []string{"", "http://rsshub.internal:1200"} {
		for _, tc := range []struct{ input, want string }{
			{"http://feeds.feedburner.com/RockPaperShotgun", "https://feeds.feedburner.com/RockPaperShotgun"},
			{"http://FEEDS.FEEDBURNER.COM:80/Some%2FFeed?format=xml&x=a%2Bb", "https://feeds.feedburner.com/Some%2FFeed?format=xml&x=a%2Bb"},
			{"https://feeds.feedburner.com/SomeFeed", "https://feeds.feedburner.com/SomeFeed"},
			{"http://feeds.feedburner.com:8080/SomeFeed", "http://feeds.feedburner.com:8080/SomeFeed"},
			{"http://feeds.feedburner.com:443/SomeFeed", "http://feeds.feedburner.com:443/SomeFeed"},
			{"http://feeds.feedburner.com:/SomeFeed", "http://feeds.feedburner.com:/SomeFeed"},
			{"http://feeds.feedburner.com:invalid/SomeFeed", "http://feeds.feedburner.com:invalid/SomeFeed"},
			{"http://feeds.feedburner.com.evil.example/feed", "http://feeds.feedburner.com.evil.example/feed"},
			{"http://evilfeeds.feedburner.com/feed", "http://evilfeeds.feedburner.com/feed"},
			{"http://user:password@feeds.feedburner.com/feed", "http://user:password@feeds.feedburner.com/feed"},
			{"ftp://feeds.feedburner.com/feed", "ftp://feeds.feedburner.com/feed"},
			{"http://example.com/feed", "http://example.com/feed"},
		} {
			t.Run(base+"/"+tc.input, func(t *testing.T) {
				got, trusted := resolveFeedURL(tc.input, base)
				if got != tc.want || trusted {
					t.Fatalf("resolveFeedURL() = %q, trusted=%v; want %q, false", got, trusted, tc.want)
				}
			})
		}
	}
}

func TestFeedBurnerPreviewUsesAndReturnsCanonicalHTTPS(t *testing.T) {
	const input = "http://feeds.feedburner.com/RockPaperShotgun?format=xml"
	const canonical = "https://feeds.feedburner.com/RockPaperShotgun?format=xml"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != canonical {
			t.Errorf("preview requested %q, want %q", req.URL, canonical)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/rss+xml"}}, Body: io.NopCloser(strings.NewReader(weiboRSSBody)), Request: req}, nil
	})}
	fetcher := newPublicFetcherWithClients("", client, nil)
	got, err := fetcher.Preview(context.Background(), input)
	if err != nil || got == nil {
		t.Fatalf("Preview() = %v, %v", got, err)
	}
	if got.ActualURL != canonical || got.FeedType != "rss" || len(got.Items) == 0 {
		t.Fatalf("Preview() canonical identity/items = %+v", got)
	}
}

func TestStoredFeedBurnerFetchUpgradesHTTPSAndKeepsConditionalHeaders(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://feeds.feedburner.com/OtherFeed" {
			t.Errorf("stored feed requested %q", req.URL)
		}
		if req.Header.Get("If-None-Match") != `"feed-etag"` || req.Header.Get("If-Modified-Since") != "Wed, 04 Nov 2026 18:02:20 GMT" {
			t.Error("conditional headers were lost")
		}
		return &http.Response{StatusCode: http.StatusNotModified, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})}
	worker := NewFetcher("")
	worker.client = client
	for _, fetcher := range []*Fetcher{newPublicFetcherWithClients("", client, nil), worker} {
		got, err := fetcher.Fetch(context.Background(), "http://feeds.feedburner.com/OtherFeed", `"feed-etag"`, "Wed, 04 Nov 2026 18:02:20 GMT")
		if err != nil || got != nil {
			t.Fatalf("Fetch() = %v, %v; want unchanged feed", got, err)
		}
	}
}
