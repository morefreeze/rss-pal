package rss

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCatalogFeedCheckRequiresBoundedRSS(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{{"rss", `<rss version="2.0"><channel><title>Test</title><item><title>Entry</title><link>https://example.com/post</link></item></channel></rss>`, true}, {"empty", `<rss version="2.0"><channel><title>Test</title></channel></rss>`, false}, {"html", `<html><body>Not RSS</body></html>`, false}, {"large", strings.Repeat("x", (2<<20)+1), false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
			})}
			f := newPublicFetcherWithClients("http://rsshub:1200", c, c)
			if err := f.CheckFeed(context.Background(), "https://example.com/rss"); (err == nil) != tc.ok {
				t.Fatalf("check = %v", err)
			}
		})
	}
}
