package explore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/httpx"
)

type sourceFetchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f sourceFetchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func sourceResponse(req *http.Request, status int, contentType, body string) *http.Response {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func sourceFetcherForTest(now time.Time, rt sourceFetchRoundTripFunc) *SourceFetcher {
	return &SourceFetcher{client: &http.Client{Transport: rt}, now: func() time.Time { return now }}
}

func validRSS(items string) string {
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>` + items + `</channel></rss>`
}

func rssItem(title, link string, published time.Time) string {
	date := ""
	if !published.IsZero() {
		date = "<pubDate>" + published.Format(time.RFC1123Z) + "</pubDate>"
	}
	return "<item><title>" + title + "</title><link>" + link + "</link>" + date + "<description>summary</description><content:encoded xmlns:content=\"http://purl.org/rss/1.0/modules/content/\"><![CDATA[body]]></content:encoded></item>"
}

func validConfidenceRequest(rawURL string) SourceFetchRequest {
	return SourceFetchRequest{URL: rawURL, Mode: SourceFetchValidate, DirectProfile: true}
}

func TestSourceFetchDirectRSSAndAtom(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, contentType, body string
	}{
		{"rss", "application/rss+xml", validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-time.Hour)) + rssItem("Two", "/two", now.Add(-2*time.Hour)))},
		{"atom", "application/atom+xml", `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Feed</title><entry><title>One</title><link href="https://9.9.9.9/one"/><updated>2026-08-31T10:00:00Z</updated><summary>summary</summary><content>body</content></entry><entry><title>Two</title><link href="/two"/><updated>2026-08-30T10:00:00Z</updated></entry></feed>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
				response := sourceResponse(req, http.StatusOK, tc.contentType, tc.body)
				response.Header.Set("ETag", `"v2"`)
				response.Header.Set("Last-Modified", "Sun, 30 Aug 2026 10:00:00 GMT")
				return response, nil
			})
			result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed"))
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if result.FeedURL != "https://8.8.8.8/feed" || len(result.Articles) != 2 || result.ETag != `"v2"` || result.LastModified == "" {
				t.Fatalf("unexpected result: %#v", result)
			}
			if result.Articles[0].Title != "One" || result.Articles[1].URL != "https://8.8.8.8/two" {
				t.Fatalf("unexpected articles: %#v", result.Articles)
			}
			if result.Articles[0].Content == nil || result.Articles[0].Excerpt == nil {
				t.Fatalf("content/excerpt missing: %#v", result.Articles[0])
			}
		})
	}
}

func TestSourceFetchHTMLDiscoveryUsesFinalURLAndAtMostFourCandidates(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var paths []string
	feed := validRSS(rssItem("One", "https://9.9.9.9/one", now) + rssItem("Two", "https://9.9.9.9/two", now.Add(-time.Hour)))
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == "/start" {
			finalRequest := req.Clone(req.Context())
			finalRequest.URL, _ = url.Parse("https://8.8.8.8/final/page.html")
			html := `<html><head>` +
				`<link rel="alternate" type="application/rss+xml" href="../z.xml">` +
				`<link rel="alternate stylesheet" type="application/atom+xml; charset=utf-8" href="a.xml">` +
				`<link rel="alternate" type="application/rss+xml" href="e.xml">` +
				`<link rel="alternate" type="application/rss+xml" href="d.xml">` +
				`<link rel="alternate" type="application/rss+xml" href="c.xml">` +
				`<link rel="alternate" type="text/html" href="ignored.html">` +
				`</head></html>`
			return sourceResponse(finalRequest, http.StatusOK, "text/html", html), nil
		}
		if req.URL.Path == "/final/a.xml" {
			return sourceResponse(req, http.StatusOK, "application/atom+xml", feed), nil
		}
		return sourceResponse(req, http.StatusOK, "text/html", "<html>not a feed</html>"), nil
	})
	result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/start"))
	if err != nil {
		t.Fatalf("Fetch: %v (paths=%v)", err, paths)
	}
	if result.FeedURL != "https://8.8.8.8/final/a.xml" {
		t.Fatalf("FeedURL = %q", result.FeedURL)
	}
	if len(paths) != 2 || paths[1] != "/final/a.xml" {
		t.Fatalf("expected first sorted candidate only after success, paths=%v", paths)
	}

	paths = nil
	fetcher = sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path == "/start" {
			return sourceResponse(req, http.StatusOK, "text/html", `<link rel="alternate" type="application/rss+xml" href="/5"><link rel="alternate" type="application/rss+xml" href="/4"><link rel="alternate" type="application/rss+xml" href="/3"><link rel="alternate" type="application/rss+xml" href="/2"><link rel="alternate" type="application/rss+xml" href="/1">`), nil
		}
		return sourceResponse(req, http.StatusOK, "text/html", "not feed"), nil
	})
	_, _ = fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/start"))
	if len(paths) != 5 { // page + four candidates
		t.Fatalf("discovery fetched %d requests, paths=%v", len(paths), paths)
	}
}

func TestSourceFetchHTMLDiscoveryBoundsLargeAlternateSet(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var html strings.Builder
	for i := 999; i >= 0; i-- {
		fmt.Fprintf(&html, `<link rel="alternate" type="application/rss+xml" href="/%04d.xml">`, i)
	}
	requests := 0
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path == "/start" {
			return sourceResponse(req, http.StatusOK, "text/html", html.String()), nil
		}
		return sourceResponse(req, http.StatusOK, "text/html", "not a feed"), nil
	})
	_, _ = fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/start"))
	if requests != 1+maxDiscoveryCandidates {
		t.Fatalf("requests=%d, want initial + %d bounded candidates", requests, maxDiscoveryCandidates)
	}
}

func TestSourceFetchParsesFeedBeforeTrustingHTMLContentType(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	body := validRSS(rssItem("One", "https://9.9.9.9/one", now) + rssItem("Two", "https://9.9.9.9/two", now.Add(-time.Hour)))
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		return sourceResponse(req, http.StatusOK, "text/html", body), nil
	})
	result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed"))
	if err != nil || len(result.Articles) != 2 {
		t.Fatalf("valid feed with wrong content type rejected: result=%#v err=%v", result, err)
	}
}

func TestSourceFetchReturnsCanonicalFinalFeedURL(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	body := validRSS(rssItem("One", "https://9.9.9.9/one", now) + rssItem("Two", "https://9.9.9.9/two", now.Add(-time.Hour)))
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		return sourceResponse(req, http.StatusOK, "application/rss+xml", body), nil
	})
	result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed?utm_source=registry#fragment"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.FeedURL != "https://8.8.8.8/feed" {
		t.Fatalf("FeedURL=%q", result.FeedURL)
	}
}

func TestSourceFetchValidationAndArticleNormalization(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		body    string
		want    int
		wantErr bool
	}{
		{"zero items", validRSS(""), 0, true},
		{"one item", validRSS(rssItem("One", "https://9.9.9.9/one", now)), 0, true},
		{"all stale", validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-91*24*time.Hour)) + rssItem("Two", "https://9.9.9.9/two", now.Add(-365*24*time.Hour))), 0, true},
		{"one recent", validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-90*24*time.Hour)) + rssItem("Two", "https://9.9.9.9/two", now.Add(-365*24*time.Hour))), 2, false},
		{"future within clock skew", validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(24*time.Hour)) + rssItem("Two", "https://9.9.9.9/two", time.Time{})), 2, false},
		{"future beyond clock skew", validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(24*time.Hour+time.Second)) + rssItem("Two", "https://9.9.9.9/two", time.Time{})), 0, true},
		{"recent updated date counts", `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Feed</title><entry><title>One</title><link href="https://9.9.9.9/one"/><published>` + now.Add(-365*24*time.Hour).Format(time.RFC3339) + `</published><updated>` + now.Add(-time.Hour).Format(time.RFC3339) + `</updated></entry><entry><title>Two</title><link href="https://9.9.9.9/two"/></entry></feed>`, 2, false},
		{"reject unsafe malformed and duplicate urls", validRSS(rssItem("Good", "https://9.9.9.9/post?utm_source=x", now) + rssItem("Duplicate", "https://9.9.9.9/post", now.Add(-time.Hour)) + rssItem("Private", "http://127.0.0.1/x", now) + rssItem("Credential", "https://u:p@9.9.9.9/x", now) + rssItem("Malformed", "://", now) + rssItem("Second", "https://9.9.9.9/second", now)), 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
				return sourceResponse(req, http.StatusOK, "application/rss+xml", tc.body), nil
			})
			result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed"))
			if tc.wantErr {
				if ClassifySourceFetchError(err) != SourceFetchTerminal {
					t.Fatalf("expected terminal error, got %v", err)
				}
				return
			}
			if err != nil || len(result.Articles) != tc.want {
				t.Fatalf("got articles=%d err=%v", len(result.Articles), err)
			}
		})
	}
}

func TestSourceRefreshSkipsNewSourceConfidenceAndActivityGates(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{name: "one old article", body: validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-365*24*time.Hour))), want: 1},
		{name: "all old articles", body: validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-365*24*time.Hour)) + rssItem("Two", "https://9.9.9.9/two", now.Add(-400*24*time.Hour))), want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
				calls++
				return sourceResponse(req, http.StatusOK, "application/rss+xml", tc.body), nil
			})
			staleSuccess := now.Add(-8 * 24 * time.Hour)
			result, err := fetcher.Fetch(context.Background(), SourceFetchRequest{
				URL:  "https://8.8.8.8/feed",
				Mode: SourceFetchRefresh,
				Evidence: []ObservationEvidence{{
					ProviderID: 1, ProviderKind: "opml", Enabled: true,
					ProviderLastSuccessAt: &staleSuccess, LastSeenAt: now.Add(-31 * 24 * time.Hour), OccurrenceCount: 1,
				}},
			})
			if err != nil || len(result.Articles) != tc.want || calls != 1 {
				t.Fatalf("refresh result=%#v calls=%d err=%v", result, calls, err)
			}
		})
	}
}

func TestSourceRefreshNotModifiedDoesNotRequireCurrentEvidence(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("If-None-Match") != `"old"` {
			t.Fatalf("missing refresh validator: %v", req.Header)
		}
		return sourceResponse(req, http.StatusNotModified, "", ""), nil
	})
	result, err := fetcher.Fetch(context.Background(), SourceFetchRequest{
		URL: "https://8.8.8.8/feed", Mode: SourceFetchRefresh, ETag: `"old"`,
	})
	if err != nil || !result.NotModified {
		t.Fatalf("refresh 304 result=%#v err=%v", result, err)
	}
}

func TestSourceFetchDeterministicallyCapsFiftyArticlesAndTitleBytes(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var items strings.Builder
	for i := 0; i < 51; i++ {
		title := fmt.Sprintf("Item %02d", i)
		if i == 0 {
			title = strings.Repeat("界", 200)
		}
		items.WriteString(rssItem(title, fmt.Sprintf("https://9.9.9.9/%02d", i), now.Add(-time.Duration(i)*time.Hour)))
	}
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		return sourceResponse(req, http.StatusOK, "application/rss+xml", validRSS(items.String())), nil
	})
	result, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(result.Articles) != 50 || result.Articles[49].URL != "https://9.9.9.9/49" {
		t.Fatalf("unexpected deterministic cap: len=%d last=%q", len(result.Articles), result.Articles[49].URL)
	}
	if len(result.Articles[0].Title) > 500 || !strings.HasSuffix(result.Articles[0].Title, "界") {
		t.Fatalf("title was not UTF-8 safely clipped: %q (%d bytes)", result.Articles[0].Title, len(result.Articles[0].Title))
	}
}

func TestSourceFetchConditionalHeadersAndNotModified(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var got http.Header
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		got = req.Header.Clone()
		response := sourceResponse(req, http.StatusNotModified, "", "must not parse")
		response.Header.Set("ETag", `"new"`)
		return response, nil
	})
	request := validConfidenceRequest("https://8.8.8.8/feed")
	request.ETag = `"old"`
	request.LastModified = "Sun, 30 Aug 2026 10:00:00 GMT"
	result, err := fetcher.Fetch(context.Background(), request)
	if err != nil || !result.NotModified || len(result.Articles) != 0 || result.ETag != `"new"` {
		t.Fatalf("unexpected result=%#v err=%v", result, err)
	}
	if got.Get("If-None-Match") != `"old"` || got.Get("If-Modified-Since") != request.LastModified {
		t.Fatalf("conditional headers missing: %v", got)
	}
}

func TestSourceFetchBodyLimitAndStatusClassification(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		status       int
		body         string
		transportErr error
		want         SourceFetchErrorKind
	}{
		{"exact body parses terminal content", 200, "four", nil, SourceFetchTerminal},
		{"overflow", 200, "fives", nil, SourceFetchTerminal},
		{"408", 408, "", nil, SourceFetchRetryable},
		{"425", 425, "", nil, SourceFetchRetryable},
		{"429", 429, "", nil, SourceFetchRetryable},
		{"500", 500, "", nil, SourceFetchRetryable},
		{"404", 404, "", nil, SourceFetchTerminal},
		{"network", 0, "", errors.New("network down"), SourceFetchRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return sourceResponse(req, tc.status, "application/rss+xml", tc.body), nil
			})
			fetcher.maxBodyBytes = 4
			_, err := fetcher.Fetch(context.Background(), validConfidenceRequest("https://8.8.8.8/feed"))
			if ClassifySourceFetchError(err) != tc.want {
				t.Fatalf("kind=%q err=%v", ClassifySourceFetchError(err), err)
			}
		})
	}
	for _, raw := range []string{"http://127.0.0.1/feed", "https://u:p@8.8.8.8/feed", "https://8.8.8.8:2375/feed"} {
		fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
			t.Fatal("unexpected network request")
			return nil, nil
		})
		_, err := fetcher.Fetch(context.Background(), validConfidenceRequest(raw))
		if ClassifySourceFetchError(err) != SourceFetchTerminal {
			t.Fatalf("%s kind=%q err=%v", raw, ClassifySourceFetchError(err), err)
		}
	}
}

func TestSourceConfidenceBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	providerSuccess := now.Add(-7 * 24 * time.Hour)
	freshSeen := now.Add(-30 * 24 * time.Hour)
	base := func(id int, kind string, occurrences int) ObservationEvidence {
		return ObservationEvidence{ProviderID: id, ProviderKind: kind, Enabled: true, ProviderLastSuccessAt: &providerSuccess, LastSeenAt: freshSeen, OccurrenceCount: occurrences}
	}
	cases := []struct {
		name     string
		evidence []ObservationEvidence
		direct   bool
		want     bool
	}{
		{"direct profile", nil, true, true},
		{"structured opml", []ObservationEvidence{base(1, "opml", 1)}, false, true},
		{"structured directory", []ObservationEvidence{base(1, "directory", 1)}, false, true},
		{"structured awesome", []ObservationEvidence{base(1, "github_awesome", 1)}, false, true},
		{"two providers", []ObservationEvidence{base(1, "related_site", 1), base(2, "reddit_stream", 1)}, false, true},
		{"duplicate provider id", []ObservationEvidence{base(1, "related_site", 1), base(1, "reddit_stream", 1)}, false, false},
		{"repeated reddit", []ObservationEvidence{base(1, "reddit_stream", 2)}, false, true},
		{"repeated related", []ObservationEvidence{base(1, "related_site", 2)}, false, true},
		{"one incidental", []ObservationEvidence{base(1, "reddit_stream", 1)}, false, false},
		{"disabled", func() []ObservationEvidence {
			v := base(1, "opml", 1)
			v.Enabled = false
			return []ObservationEvidence{v}
		}(), false, false},
		{"provider older than seven days", func() []ObservationEvidence {
			v := base(1, "opml", 1)
			old := now.Add(-7*24*time.Hour - time.Nanosecond)
			v.ProviderLastSuccessAt = &old
			return []ObservationEvidence{v}
		}(), false, false},
		{"observation older than thirty days", func() []ObservationEvidence {
			v := base(1, "opml", 1)
			v.LastSeenAt = now.Add(-30*24*time.Hour - time.Nanosecond)
			return []ObservationEvidence{v}
		}(), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasSourceConfidence(now, tc.evidence, tc.direct); got != tc.want {
				t.Fatalf("HasSourceConfidence=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSourceFetcherInsufficientConfidenceIsTerminalAndDetectable(t *testing.T) {
	fetcher := sourceFetcherForTest(time.Now(), func(request *http.Request) (*http.Response, error) {
		t.Fatal("insufficient confidence must fail before HTTP")
		return nil, nil
	})
	_, err := fetcher.Fetch(context.Background(), SourceFetchRequest{URL: "https://example.com/feed"})
	if !errors.Is(err, ErrInsufficientSourceConfidence) {
		t.Fatalf("err=%v does not wrap ErrInsufficientSourceConfidence", err)
	}
	if got := ClassifySourceFetchError(err); got != SourceFetchTerminal {
		t.Fatalf("classification=%q", got)
	}
}

func TestSourceFetchProviderEvidenceCannotBypassFreshness(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	success := now
	request := SourceFetchRequest{URL: "https://8.8.8.8/feed", Evidence: []ObservationEvidence{{ProviderID: 1, ProviderKind: "opml", Enabled: true, ProviderLastSuccessAt: &success, LastSeenAt: now, OccurrenceCount: 1}}}
	stale := validRSS(rssItem("One", "https://9.9.9.9/one", now.Add(-91*24*time.Hour)) + rssItem("Two", "https://9.9.9.9/two", now.Add(-92*24*time.Hour)))
	fetcher := sourceFetcherForTest(now, func(req *http.Request) (*http.Response, error) {
		return sourceResponse(req, http.StatusOK, "application/rss+xml", stale), nil
	})
	_, err := fetcher.Fetch(context.Background(), request)
	if ClassifySourceFetchError(err) != SourceFetchTerminal {
		t.Fatalf("expected stale source to remain terminal, got %v", err)
	}
}

func TestSourceFetchErrorClassificationAcceptsWrappedHTTPBodyLimit(t *testing.T) {
	err := fmt.Errorf("fetch: %w", httpx.ErrResponseTooLarge)
	if got := ClassifySourceFetchError(err); got != SourceFetchTerminal {
		t.Fatalf("kind=%q", got)
	}
}
