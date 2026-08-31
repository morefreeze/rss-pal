package explore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestProviderClientUsesTrustedDoerOnlyForRootRelativeRSSHubEndpoint(t *testing.T) {
	publicCalls, trustedCalls, validationCalls := 0, 0, 0
	publicDoer := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		publicCalls++
		return nil, errors.New("public doer must not be used")
	})
	trustedDoer := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		trustedCalls++
		if got, want := request.URL.String(), "http://rsshub:1200/reddit/subreddit/golang?limit=20"; got != want {
			t.Fatalf("trusted request URL = %q, want %q", got, want)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("rsshub"))}, nil
	})
	client := newProviderClientWithTrustedForTest(publicDoer, trustedDoer, func(raw string) (*url.URL, error) {
		validationCalls++
		return nil, errors.New("public validator must not be used")
	}, "http://rsshub:1200", 32, "")

	result, err := client.Fetch(t.Context(), "/reddit/subreddit/golang?limit=20", "", "")
	if err != nil || string(result.Body) != "rsshub" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if publicCalls != 0 || validationCalls != 0 || trustedCalls != 1 {
		t.Fatalf("public calls = %d, validation calls = %d, trusted calls = %d", publicCalls, validationCalls, trustedCalls)
	}
}

func TestProviderClientAbsolutePrivateEndpointNeverUsesTrustedRSSHubBypass(t *testing.T) {
	publicCalls, trustedCalls, validationCalls := 0, 0, 0
	client := newProviderClientWithTrustedForTest(
		roundTripperFunc(func(*http.Request) (*http.Response, error) {
			publicCalls++
			return nil, errors.New("unexpected request")
		}),
		roundTripperFunc(func(*http.Request) (*http.Response, error) {
			trustedCalls++
			return nil, errors.New("unexpected request")
		}),
		func(string) (*url.URL, error) { validationCalls++; return nil, errors.New("blocked address") },
		"http://rsshub:1200", 32, "",
	)

	if _, err := client.Fetch(t.Context(), "http://127.0.0.1/private-feed", "", ""); err == nil {
		t.Fatal("absolute private provider URL unexpectedly succeeded")
	}
	if validationCalls != 1 || publicCalls != 0 || trustedCalls != 0 {
		t.Fatalf("validation calls = %d, public calls = %d, trusted calls = %d", validationCalls, publicCalls, trustedCalls)
	}
}

func TestProviderClientRejectsInvalidRSSHubBaseBeforeRequest(t *testing.T) {
	for _, base := range []string{
		"rsshub:1200",
		"ftp://rsshub:1200",
		"http://user:secret@rsshub:1200",
		"http:///missing-host",
		"http://rsshub:0",
		"http://rsshub:bad",
		"http://rsshub:99999",
		"http://rsshub:1200/?token=secret",
		"http://rsshub:1200/#fragment",
	} {
		t.Run(base, func(t *testing.T) {
			client := NewProviderClient(base)
			if _, err := client.Fetch(t.Context(), "/reddit/r/golang", "", ""); err == nil {
				t.Fatalf("invalid base %q unexpectedly succeeded", base)
			}
		})
	}
}

func TestProviderClientRejectsRSSHubAuthorityEscapesBeforeRequest(t *testing.T) {
	called := false
	client := newProviderClientWithTrustedForTest(
		roundTripperFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected request")
		}),
		roundTripperFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected request")
		}),
		func(raw string) (*url.URL, error) { return url.Parse(raw) },
		"http://rsshub:1200", 32, "",
	)
	for _, endpoint := range []string{
		"//evil.example/feed",
		`/\\evil.example/feed`,
		"/%2f%2fevil.example/feed",
		"/feed#fragment",
		"user:secret@evil.example/feed",
	} {
		if _, err := client.Fetch(t.Context(), endpoint, "", ""); err == nil {
			t.Errorf("authority escape %q unexpectedly succeeded", endpoint)
		}
	}
	if called {
		t.Fatal("malicious relative endpoint reached a provider doer")
	}
}

func TestProviderClientTrustedRSSHubRedirectsStayOnConfiguredOrigin(t *testing.T) {
	crossOrigin := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("cross-origin redirect must not be followed")
	}))
	defer crossOrigin.Close()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/same":
			http.Redirect(w, request, "/final", http.StatusFound)
		case "/final":
			_, _ = io.WriteString(w, "same-origin")
		case "/cross":
			http.Redirect(w, request, crossOrigin.URL+"/feed", http.StatusFound)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	result, err := client.Fetch(t.Context(), "/same", "", "")
	if err != nil || string(result.Body) != "same-origin" {
		t.Fatalf("same-origin result = %#v, err = %v", result, err)
	}
	if _, err := client.Fetch(t.Context(), "/cross", "", ""); err == nil {
		t.Fatal("cross-origin RSSHub redirect unexpectedly succeeded")
	}
}

func TestProviderClientTrustedRSSHubPreservesConditionalAndStatusHandling(t *testing.T) {
	doer := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("If-None-Match"); got != `"old"` {
			t.Fatalf("If-None-Match = %q", got)
		}
		if got := request.Header.Get("If-Modified-Since"); got != "yesterday" {
			t.Fatalf("If-Modified-Since = %q", got)
		}
		status := http.StatusNotModified
		if request.URL.Path == "/unavailable" {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ignored"))}, nil
	})
	client := newProviderClientWithTrustedForTest(nil, doer, nil, "http://rsshub:1200", 32, "")

	result, err := client.Fetch(t.Context(), "/not-modified", `"old"`, "yesterday")
	if err != nil || !result.NotModified || len(result.Body) != 0 {
		t.Fatalf("304 result = %#v, err = %v", result, err)
	}
	if _, err := client.Fetch(t.Context(), "/unavailable", `"old"`, "yesterday"); err == nil {
		t.Fatal("trusted RSSHub non-2xx response unexpectedly succeeded")
	}
}

func TestNormalizeCandidatesRejectsUnsafeURLsAndDeduplicatesDeterministically(t *testing.T) {
	candidates := NormalizeCandidates([]Candidate{
		{ExternalKey: "two", FeedURL: "https://Example.com/Feed?utm_source=test", Title: "second", OccurrenceCount: 2},
		{ExternalKey: "one", FeedURL: "https://example.com/Feed#top", Title: "first", Tags: []string{"go"}},
		{ExternalKey: "bad", FeedURL: "ftp://example.com/feed"},
		{ExternalKey: "creds", FeedURL: "https://user:secret@example.com/feed"},
	})

	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if got, want := candidates[0].FeedURL, "https://example.com/Feed"; got != want {
		t.Errorf("FeedURL = %q, want %q", got, want)
	}
	if got, want := candidates[0].OccurrenceCount, 3; got != want {
		t.Errorf("OccurrenceCount = %d, want %d", got, want)
	}
	if got, want := candidates[0].ExternalKey, "one"; got != want {
		t.Errorf("ExternalKey = %q, want %q", got, want)
	}
}

func TestNormalizeCandidatesKeepsLexicallySmallestCanonicalFeedURLsRegardlessOfInputOrder(t *testing.T) {
	input := make([]Candidate, 0, 2100)
	for i := 2099; i >= 0; i-- {
		input = append(input, Candidate{ExternalKey: fmt.Sprintf("key-%04d", i), FeedURL: fmt.Sprintf("https://feeds.example/%04d?utm_source=x", i)})
	}
	reversed := append([]Candidate(nil), input...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	got, gotReversed := NormalizeCandidates(input), NormalizeCandidates(reversed)
	if len(got) != maxProviderCandidates || got[0].FeedURL != "https://feeds.example/0000" || got[len(got)-1].FeedURL != "https://feeds.example/1999" {
		t.Fatalf("normalized candidates = %d, first/last = %#v / %#v", len(got), got[0], got[len(got)-1])
	}
	if !reflect.DeepEqual(got, gotReversed) {
		t.Fatal("NormalizeCandidates depends on input order")
	}
}

func TestProviderBodyLimitRejectsFourMiBPlusOne(t *testing.T) {
	if err := checkProviderBody(make([]byte, defaultProviderBodyBytes+1)); err == nil {
		t.Fatal("checkProviderBody accepted body over four MiB")
	}
}

func TestProviderClientSendsConditionalHeadersAndHandles304(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != `"old"` {
			t.Errorf("If-None-Match = %q", got)
		}
		if got := r.Header.Get("If-Modified-Since"); got != "yesterday" {
			t.Errorf("If-Modified-Since = %q", got)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	client := testProviderClient(server.Client())
	result, err := client.Fetch(context.Background(), server.URL, `"old"`, "yesterday")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !result.NotModified || len(result.Body) != 0 {
		t.Errorf("result = %#v", result)
	}
}

func TestProviderClientRejectsOversizedAndResolvesOnlyConfiguredRSSHubPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 9))
	}))
	defer server.Close()
	client := newProviderClientForTest(server.Client(), func(raw string) (*url.URL, error) { return url.Parse(raw) }, "", 8, "")
	if _, err := client.Fetch(context.Background(), server.URL, "", ""); err == nil {
		t.Fatal("Fetch() oversized body unexpectedly succeeded")
	}
	withBase := newProviderClientForTest(server.Client(), func(raw string) (*url.URL, error) { return url.Parse(raw) }, server.URL, 8, "")
	if _, err := withBase.Fetch(context.Background(), "/reddit/subreddit/golang", "", ""); err == nil {
		t.Fatal("relative RSSHub request should still surface oversized body")
	}
	if _, err := client.Fetch(context.Background(), "/reddit/subreddit/golang", "", ""); err == nil {
		t.Fatal("relative endpoint without RSSHub base unexpectedly succeeded")
	}
}

func TestProviderClientRejectsEndpointCredentialsBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("request must not be sent") }))
	defer server.Close()
	client := testProviderClient(server.Client())
	endpoint := strings.Replace(server.URL, "://", "://user:secret@", 1)
	if _, err := client.Fetch(context.Background(), endpoint, "", ""); err == nil {
		t.Fatal("credential endpoint unexpectedly succeeded")
	}
}

func testProviderClient(client *http.Client) ProviderClient {
	return newProviderClientForTest(client, func(raw string) (*url.URL, error) { return url.Parse(raw) }, "", 4<<20, "")
}

// newProviderClientForTest is test-only dependency injection for httptest;
// production callers must use NewProviderClient.
func newProviderClientForTest(doer interface {
	Do(*http.Request) (*http.Response, error)
}, validator func(string) (*url.URL, error), rssHubBaseURL string, maxBodyBytes int64, userAgent string) ProviderClient {
	return newProviderClientWithTrustedForTest(doer, doer, validator, rssHubBaseURL, maxBodyBytes, userAgent)
}

func newProviderClientWithTrustedForTest(publicDoer, trustedDoer interface {
	Do(*http.Request) (*http.Response, error)
}, validator func(string) (*url.URL, error), rssHubBaseURL string, maxBodyBytes int64, userAgent string) ProviderClient {
	origin, baseErr := parseRSSHubBaseURL(rssHubBaseURL)
	return ProviderClient{publicDoer: publicDoer, trustedDoer: trustedDoer, validateURL: validator, rssHubOrigin: origin, rssHubBaseErr: baseErr, maxBodyBytes: maxBodyBytes, userAgent: userAgent}
}
