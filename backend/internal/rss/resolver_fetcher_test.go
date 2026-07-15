package rss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetcherFetchUsesResolvedFeedURL(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var requestedURL string
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rss version="2.0"><channel><title>CSDN</title>` +
				`<link>https://blog.csdn.net/csdngeeknews</link><description>feed</description>` +
				`<item><title>post</title><link>https://example.com/post</link><guid>post</guid></item>` +
				`</channel></rss>`)),
		}, nil
	})}

	result, err := fetcher.Fetch(context.Background(), "https://blog.csdn.net/csdngeeknews", "", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if requestedURL != "http://rsshub.test:1200/csdn/blog/csdngeeknews" {
		t.Fatalf("requested URL = %q", requestedURL)
	}
	if result == nil || result.Feed == nil || result.Feed.Title != "CSDN" {
		t.Fatalf("unexpected fetch result: %#v", result)
	}
}
