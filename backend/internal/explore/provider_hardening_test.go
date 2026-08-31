package explore

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
	"unicode/utf8"
)

func TestProviderAdaptersRejectPrivateLiteralCandidates(t *testing.T) {
	tests := []struct {
		name  string
		parse func() ([]Candidate, error)
	}{
		{"opml", func() ([]Candidate, error) {
			return (OPMLRegistryAdapter{}).Parse(Provider{}, []byte(`<opml><body><outline xmlUrl="http://127.0.0.1/feed"/></body></opml>`))
		}},
		{"directory", func() ([]Candidate, error) {
			return (DirectoryAdapter{}).Parse(Provider{Endpoint: "https://directory.example/feed"}, []byte(`<rss><channel><item><link>http://[::1]/feed</link></item></channel></rss>`))
		}},
		{"reddit", func() ([]Candidate, error) {
			return (RedditLinkStreamAdapter{}).Parse(Provider{}, []byte(`<rss><channel><item><description>http://localhost/feed</description></item></channel></rss>`))
		}},
		{"related feed", func() ([]Candidate, error) {
			return (RelatedSiteDiscoverer{}).Discover("https://site.example/article", []byte(`<link rel="alternate" type="application/rss+xml" href="http://169.254.169.254/feed">`))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.parse()
			if err != nil || len(got) != 0 {
				t.Fatalf("candidates=%#v err=%v", got, err)
			}
		})
	}
}

func TestProviderClientTestConstructorInjectsDependencies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer server.Close()
	client := newProviderClientForTest(server.Client(), func(raw string) (*url.URL, error) { return url.Parse(raw) }, "", 8, "test-agent")
	result, err := client.Fetch(t.Context(), server.URL, "", "")
	if err != nil || string(result.Body) != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestIsRegistryProviderStaleAtSevenDayBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if !IsRegistryProviderStale(RegistryProvider{}, now) {
		t.Fatal("nil success must be stale")
	}
	exact := now.Add(-7 * 24 * time.Hour)
	if IsRegistryProviderStale(RegistryProvider{LastSuccessAt: &exact}, now) {
		t.Fatal("exact seven days must be fresh")
	}
	old := exact.Add(-time.Nanosecond)
	if !IsRegistryProviderStale(RegistryProvider{LastSuccessAt: &old}, now) {
		t.Fatal("older than seven days must be stale")
	}
}

func TestRegistrySyncDueRejectsMissingQueueBeforeLoadingProviders(t *testing.T) {
	store := &registryStoreStub{}
	_, err := (Registry{Store: store}).SyncDue(t.Context(), time.Now())
	if err == nil || err.Error() != "registry queue is required" {
		t.Fatalf("err=%v", err)
	}
}

func TestRelatedFeedDeclarationRequiresAlternateRelToken(t *testing.T) {
	for _, rel := range []string{"notalternate", "alternatefoo"} {
		got, err := (RelatedSiteDiscoverer{}).Discover("https://site.example/article", []byte(`<link rel="`+rel+`" type="application/rss+xml" href="/feed.xml">`))
		if err != nil || len(got) != 0 {
			t.Fatalf("rel=%q candidates=%#v err=%v", rel, got, err)
		}
	}
	got, err := (RelatedSiteDiscoverer{}).Discover("https://site.example/article", []byte(`<link rel="stylesheet alternate" type="application/rss+xml" href="/feed.xml">`))
	if err != nil || len(got) != 1 {
		t.Fatalf("alternate token candidates=%#v err=%v", got, err)
	}
}

func TestProviderClientRejectsNetworkPathBeforeFetch(t *testing.T) {
	called := false
	client := newProviderClientForTest(roundTripperFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil }), func(raw string) (*url.URL, error) { return url.Parse(raw) }, "https://rsshub.example", 8, "")
	if _, err := client.Fetch(t.Context(), "//attacker.example/x", "", ""); err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestNormalizeCandidatesBoundsPublicFieldsAndProviderSize(t *testing.T) {
	longKey := string(make([]byte, 501))
	input := []Candidate{{ExternalKey: longKey, FeedURL: "https://example.com/feed", SiteURL: "https://example.com/" + string(make([]byte, 2050)), Title: string([]rune{'界'}) + string(make([]byte, 600)), Topic: string([]rune{'界'}) + string(make([]byte, 120)), Tags: []string{string([]rune{'界'}) + string(make([]byte, 120))}, OccurrenceCount: 0}}
	for i := 0; i < 3000; i++ {
		input = append(input, Candidate{ExternalKey: fmt.Sprintf("%d", i), FeedURL: fmt.Sprintf("https://many.example/%04d", i), Title: "many"})
	}
	got := NormalizeCandidates(input)
	if len(got) != 2000 || got[0].FeedURL != "https://example.com/feed" {
		t.Fatalf("count/ordering=%d %#v", len(got), got[0])
	}
	first := got[0]
	if len(first.ExternalKey) != len("sha256:")+64 || first.SiteURL != "" || len(first.Title) > 500 || !utf8.ValidString(first.Title) || len(first.Topic) > 100 || !utf8.ValidString(first.Topic) || len(first.Tags) != 1 || len(first.Tags[0]) > 100 || !utf8.ValidString(first.Tags[0]) || first.OccurrenceCount != 1 {
		t.Fatalf("bounded=%#v", first)
	}
	if NormalizeCandidates([]Candidate{{ExternalKey: longKey, FeedURL: "https://example.com/feed"}})[0].ExternalKey != first.ExternalKey {
		t.Fatal("long external key hash is not stable")
	}
	if got := NormalizeCandidates([]Candidate{{ExternalKey: "x", FeedURL: "https://example.com/" + string(make([]byte, 2049))}}); len(got) != 0 {
		t.Fatalf("oversized feed=%#v", got)
	}
}

func TestRelatedSiteDiscovererCapsDeclaredFeeds(t *testing.T) {
	doc := ""
	for i := 0; i < 25; i++ {
		doc += fmt.Sprintf(`<link rel="alternate" type="application/rss+xml" href="/feed/%02d">`, i)
	}
	got, err := (RelatedSiteDiscoverer{}).Discover("https://site.example/article", []byte(doc))
	if err != nil || len(got) != 20 || got[0].FeedURL != "https://site.example/feed/00" {
		t.Fatalf("candidates=%#v err=%v", got, err)
	}
}

func TestRegistrySuccessFallbackAndFailureErrorsAreJoined(t *testing.T) {
	original, persist := errors.New("original"), errors.New("persist")
	for _, status := range []int{http.StatusOK, http.StatusNotModified} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			store := &registryStoreStub{providers: []RegistryProvider{{ID: 1, Key: "p", Kind: "good", Endpoint: server.URL, ETag: `"old"`, LastModified: "old-date"}}, successErr: original, failureErr: persist}
			registry := Registry{Store: store, Queue: &registryQueueStub{}, Client: testProviderClient(server.Client()), Adapters: map[string]ProviderAdapter{"good": adapterStub{kind: "good"}}}
			results, err := registry.SyncDue(t.Context(), time.Now())
			if err != nil || len(results) != 1 || !errors.Is(results[0].Err, original) || !errors.Is(results[0].Err, persist) || len(store.failures) != 1 {
				t.Fatalf("results=%#v err=%v store=%#v", results, err, store)
			}
			if got := store.successETags[0]; got != `"old"` {
				t.Fatalf("etag fallback=%q", got)
			}
		})
	}
	adapterErr, failurePersistErr := errors.New("adapter failed"), errors.New("failure persist failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("body")) }))
	defer server.Close()
	store := &registryStoreStub{providers: []RegistryProvider{{ID: 2, Key: "bad", Kind: "bad", Endpoint: server.URL}}, failureErr: failurePersistErr}
	results, err := (Registry{Store: store, Queue: &registryQueueStub{}, Client: testProviderClient(server.Client()), Adapters: map[string]ProviderAdapter{"bad": adapterStub{kind: "bad", err: adapterErr}}}).SyncDue(t.Context(), time.Now())
	if err != nil || !errors.Is(results[0].Err, adapterErr) || !errors.Is(results[0].Err, failurePersistErr) || len(store.failures) != 1 {
		t.Fatalf("results=%#v err=%v store=%#v", results, err, store)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) Do(request *http.Request) (*http.Response, error) { return fn(request) }
