package explore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

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
	return ProviderClient{doer: doer, validateURL: validator, rssHubBaseURL: rssHubBaseURL, maxBodyBytes: maxBodyBytes, userAgent: userAgent}
}
