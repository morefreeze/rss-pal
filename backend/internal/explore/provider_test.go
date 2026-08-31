package explore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	client := testProviderClient(server.Client())
	client.MaxBodyBytes = 8
	if _, err := client.Fetch(context.Background(), server.URL, "", ""); err == nil {
		t.Fatal("Fetch() oversized body unexpectedly succeeded")
	}
	client.RSSHubBaseURL = server.URL
	if _, err := client.Fetch(context.Background(), "/reddit/subreddit/golang", "", ""); err == nil {
		t.Fatal("relative RSSHub request should still surface oversized body")
	}
	client.RSSHubBaseURL = ""
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
	return ProviderClient{Client: client, ValidateURL: func(raw string) (*url.URL, error) { return url.Parse(raw) }, MaxBodyBytes: 4 << 20}
}
