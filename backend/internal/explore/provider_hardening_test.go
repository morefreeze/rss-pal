package explore

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
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
