package rss

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingReadCloser struct {
	reader    io.Reader
	bytesRead int
	closed    bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += n
	return n, err
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type requestSnapshot struct {
	path           string
	etag           string
	lastModified   string
	userAgent      string
	accept         string
	acceptLanguage string
}

func snapshotRequest(req *http.Request) requestSnapshot {
	return requestSnapshot{
		path:           req.URL.Path,
		etag:           req.Header.Get("If-None-Match"),
		lastModified:   req.Header.Get("If-Modified-Since"),
		userAgent:      req.Header.Get("User-Agent"),
		accept:         req.Header.Get("Accept"),
		acceptLanguage: req.Header.Get("Accept-Language"),
	}
}

const weiboRSSBody = `<?xml version="1.0"?><rss version="2.0"><channel><title>Weibo</title>` +
	`<link>https://weibo.com/u/1195230310</link><description>feed</description>` +
	`<item><title>post</title><link>https://weibo.com/1195230310/post</link><guid>post</guid></item>` +
	`</channel></rss>`

func TestFetcherFetchFallsBackWhenWeiboCommentsRouteFails(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	const (
		requestETag         = `"previous-etag"`
		requestLastModified = "Wed, 16 Jul 2025 08:00:00 GMT"
		responseETag        = `"fallback-etag"`
		responseModified    = "Thu, 17 Jul 2025 09:00:00 GMT"
		failedPayload       = "upstream comments route failed"
	)
	failedBody := &trackingReadCloser{reader: strings.NewReader(failedPayload)}
	closedBeforeFallback := false
	var requests []requestSnapshot
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, snapshotRequest(req))
		if strings.HasSuffix(req.URL.Path, "/displayComments=1") {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       failedBody,
			}, nil
		}
		closedBeforeFallback = failedBody.closed
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":  []string{"application/rss+xml"},
				"Etag":          []string{responseETag},
				"Last-Modified": []string{responseModified},
			},
			Body: io.NopCloser(strings.NewReader(weiboRSSBody)),
		}, nil
	})}

	result, err := fetcher.Fetch(context.Background(), "https://weibo.com/u/1195230310", requestETag, requestLastModified)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	wantRequests := []requestSnapshot{
		{path: "/weibo/user/1195230310/displayComments=1", etag: requestETag, lastModified: requestLastModified, userAgent: userAgent},
		{path: "/weibo/user/1195230310", etag: requestETag, lastModified: requestLastModified, userAgent: userAgent},
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	if !closedBeforeFallback || !failedBody.closed {
		t.Fatalf("failed response body close state = before fallback %t, final %t", closedBeforeFallback, failedBody.closed)
	}
	if failedBody.bytesRead != len(failedPayload) {
		t.Fatalf("failed response body bytes read = %d, want %d", failedBody.bytesRead, len(failedPayload))
	}
	if result == nil || result.Feed == nil || result.Feed.Title != "Weibo" {
		t.Fatalf("unexpected fetch result: %#v", result)
	}
	if result.ETag != responseETag || result.LastModified != responseModified {
		t.Fatalf("response validators = (%q, %q), want (%q, %q)", result.ETag, result.LastModified, responseETag, responseModified)
	}
}

func TestFetcherPreviewFallsBackWhenWeiboCommentsRouteFails(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var requests []requestSnapshot
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, snapshotRequest(req))
		if strings.HasSuffix(req.URL.Path, "/displayComments=1") {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader("bad gateway")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body:       io.NopCloser(strings.NewReader(weiboRSSBody)),
		}, nil
	})}

	result, err := fetcher.Preview(context.Background(), "https://weibo.com/u/1195230310")
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	const (
		accept         = "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.9"
		acceptLanguage = "zh-CN,zh;q=0.9,en;q=0.8"
	)
	wantRequests := []requestSnapshot{
		{path: "/weibo/user/1195230310/displayComments=1", userAgent: userAgent, accept: accept, acceptLanguage: acceptLanguage},
		{path: "/weibo/user/1195230310", userAgent: userAgent, accept: accept, acceptLanguage: acceptLanguage},
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	if result == nil || result.FeedTitle != "Weibo" {
		t.Fatalf("unexpected preview result: %#v", result)
	}
}

func TestFetcherFetchDoesNotFallBackOnTransportError(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	sentinel := errors.New("transport failed")
	requestCount := 0
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		return nil, sentinel
	})}

	_, err := fetcher.Fetch(context.Background(), "https://weibo.com/u/1195230310", "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fetch() error = %v, want errors.Is sentinel", err)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestFetcherFetchRequestCreationErrorDoesNotReachTransport(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	requestCount := 0
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		return nil, errors.New("unexpected transport call")
	})}

	_, err := fetcher.Fetch(context.Background(), "://invalid", "", "")
	if err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Fatalf("Fetch() error = %v, want request-creation error", err)
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestFetcherFetchDoesNotFallBackForExternalWeiboRouteLookalike(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var requestedPaths []string
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedPaths = append(requestedPaths, req.URL.Path)
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("bad gateway")),
		}, nil
	})}

	_, err := fetcher.Fetch(context.Background(), "https://example.com/weibo/user/123/displayComments=1", "", "")
	if err == nil {
		t.Fatal("Fetch() error = nil, want non-nil")
	}
	wantPaths := []string{"/weibo/user/123/displayComments=1"}
	if !reflect.DeepEqual(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}
}

func TestWeiboCommentsFallbackURLBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		rsshubBase string
		want       string
		wantOK     bool
	}{
		{name: "wrong_scheme", raw: "https://rsshub.test:1200/weibo/user/123/displayComments=1", rsshubBase: "http://rsshub.test:1200"},
		{name: "wrong_host", raw: "http://example.com/weibo/user/123/displayComments=1", rsshubBase: "http://rsshub.test:1200"},
		{name: "configured_base_path_mismatch", raw: "http://rsshub.test:1200/weibo/user/123/displayComments=1", rsshubBase: "http://rsshub.test:1200/rsshub"},
		{name: "nonnumeric_uid", raw: "http://rsshub.test:1200/rsshub/weibo/user/not-a-uid/displayComments=1", rsshubBase: "http://rsshub.test:1200/rsshub"},
		{name: "extra_path_segments", raw: "http://rsshub.test:1200/rsshub/weibo/user/123/extra/displayComments=1", rsshubBase: "http://rsshub.test:1200/rsshub"},
		{
			name:       "exact_valid_route",
			raw:        "http://rsshub.test:1200/rsshub/weibo/user/123/displayComments=1?format=full",
			rsshubBase: "http://rsshub.test:1200/rsshub/",
			want:       "http://rsshub.test:1200/rsshub/weibo/user/123?format=full",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := weiboCommentsFallbackURL(tt.raw, tt.rsshubBase)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("weiboCommentsFallbackURL(%q, %q) = (%q, %t), want (%q, %t)", tt.raw, tt.rsshubBase, got, ok, tt.want, tt.wantOK)
			}
		})
	}
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

func TestFetcherFetchHTMLUsesResolvedFeedURL(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var requestedURL string
	requestCount := 0
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
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

	feed, err := fetcher.FetchHTML(context.Background(), "https://blog.csdn.net/csdngeeknews")
	if err != nil {
		t.Fatalf("FetchHTML() error = %v", err)
	}
	if requestedURL != "http://rsshub.test:1200/csdn/blog/csdngeeknews" {
		t.Fatalf("requested URL = %q", requestedURL)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
	if feed == nil || feed.Title != "CSDN" {
		t.Fatalf("unexpected feed: %#v", feed)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "post" {
		t.Fatalf("unexpected items: %#v", feed.Items)
	}
}
