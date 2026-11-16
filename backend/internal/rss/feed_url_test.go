package rss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
	worker.directFeedClient = nil
	for _, fetcher := range []*Fetcher{newPublicFetcherWithClients("", client, nil), worker} {
		got, err := fetcher.Fetch(context.Background(), "http://feeds.feedburner.com/OtherFeed", `"feed-etag"`, "Wed, 04 Nov 2026 18:02:20 GMT")
		if err != nil || got != nil {
			t.Fatalf("Fetch() = %v, %v; want unchanged feed", got, err)
		}
	}
}

func TestExternalAtomFeedUsesDirectRoute(t *testing.T) {
	const feedURL = "https://feed.example/feed.xml"
	const atom = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>feed.example</title><id>https://feed.example/feed.xml</id><entry><title>Test post</title><id>https://feed.example/posts/test.html</id><link href="https://feed.example/posts/test.html"/></entry></feed>`
	regular := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("regular route received %s", req.URL)
		return nil, nil
	})}
	direct := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != feedURL {
			t.Fatalf("direct route received %s", req.URL)
		}
		if req.Header.Get("If-None-Match") == `"unchanged"` {
			return &http.Response{StatusCode: http.StatusNotModified, Body: http.NoBody, Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(atom)), Request: req}, nil
	})
	public := NewPublicFetcher("")
	public.publicClient = regular
	worker := NewFetcher("")
	worker.client = regular
	for _, fetcher := range []*Fetcher{public, worker} {
		if fetcher.directFeedClient.Timeout != 20*time.Second {
			t.Fatalf("direct timeout = %v, want 20s", fetcher.directFeedClient.Timeout)
		}
		fetcher.directFeedClient.Transport = direct
	}
	preview, err := public.Preview(context.Background(), feedURL)
	if err != nil || preview.FeedType != "rss" || preview.ActualURL != feedURL || len(preview.Items) != 1 {
		t.Fatalf("Preview() = %+v, %v", preview, err)
	}
	for _, fetcher := range []*Fetcher{public, worker} {
		result, err := fetcher.Fetch(context.Background(), feedURL, "", "")
		if err != nil || result.Feed.Title != "feed.example" || len(result.Feed.Items) != 1 {
			t.Fatalf("Fetch() = %+v, %v", result, err)
		}
		result, err = fetcher.Fetch(context.Background(), feedURL, `"unchanged"`, "")
		if result != nil || err != nil {
			t.Fatalf("conditional Fetch() = %+v, %v", result, err)
		}
	}
}

func TestExternalAtomFeedFallbackKeepsConditionalHeaders(t *testing.T) {
	const feedURL = "https://feed.example/feed.xml"
	const etag = `"feed-etag"`
	const modified = "Wed, 04 Nov 2026 18:02:20 GMT"
	fetcher := NewPublicFetcher("")
	fetcher.directFeedClient.Timeout = 10 * time.Millisecond
	fetcher.directFeedClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	fetcher.publicClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != feedURL || req.Header.Get("If-None-Match") != etag || req.Header.Get("If-Modified-Since") != modified {
			t.Fatalf("fallback request URL=%s headers=%v", req.URL, req.Header)
		}
		return &http.Response{StatusCode: http.StatusNotModified, Body: http.NoBody, Request: req}, nil
	})}
	if result, err := fetcher.Fetch(context.Background(), feedURL, etag, modified); result != nil || err != nil {
		t.Fatalf("Fetch() = %+v, %v", result, err)
	}
}

func TestFeedDiscoveryReservesTimeForProxy(t *testing.T) {
	const home = "https://feed.example"
	const discovered = home + "/feed.xml"
	response := func(req *http.Request, status int, mime, body string) *http.Response {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {mime}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
	}
	fetcher := NewPublicFetcher("")
	fetcher.directFeedClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case home:
			return response(req, 200, "text/html", "<html><title>Blog</title></html>"), nil
		case discovered:
			<-req.Context().Done()
			return nil, req.Context().Err()
		default:
			return response(req, 404, "text/plain", ""), nil
		}
	})
	fetcher.publicClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == discovered {
			return response(req, 200, "application/rss+xml", weiboRSSBody), nil
		}
		return response(req, 404, "text/plain", ""), nil
	})
	preview, err := fetcher.Preview(context.Background(), home)
	if err != nil || preview.FeedType != "rss" || preview.DiscoveredRSSURL != discovered || len(preview.Items) != 1 {
		t.Fatalf("Preview() = %+v, %v", preview, err)
	}
}
