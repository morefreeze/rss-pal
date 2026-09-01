package explore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRelatedSiteDiscovererPrefersDeclaredFeedAndBoundsExternalSites(t *testing.T) {
	const document = `<html><head>
 <link rel="alternate" type="application/rss+xml" href="/feed.xml">
 </head><body>
 <a href="https://same.example/about">same</a>
 <a href="https://blog.example/post">blog</a>
 <a href="https://blog.example/other">dup</a>
 <a href="mailto:hi@example.com">mail</a><a href="https://assets.example/a.css">asset</a>
 </body></html>`
	got, err := (RelatedSiteDiscoverer{}).Discover("https://same.example/article", []byte(document))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate count = %d, want 2: %#v", len(got), got)
	}
	if got[0].FeedURL != "https://same.example/feed.xml" || got[0].SiteURL != "https://same.example/article" {
		t.Errorf("declared feed = %#v", got[0])
	}
	if got[1].FeedURL != "https://blog.example/other" || got[1].ExternalKey != "blog.example" || got[1].OccurrenceCount != 2 {
		t.Errorf("external site = %#v", got[1])
	}
}

func TestRelatedSiteDiscovererBoundsAndOrdersDeclaredAndExternalCandidates(t *testing.T) {
	declared, external := make([]string, 0, 25), make([]string, 0, 15)
	for i := 24; i >= 0; i-- {
		declared = append(declared, fmt.Sprintf(`<link rel="alternate" type="application/rss+xml" href="/feed/%02d">`, i))
	}
	for i := 14; i >= 0; i-- {
		external = append(external, fmt.Sprintf(`<a href="https://outside-%02d.example/path">outside</a>`, i))
	}
	forward := strings.Join(append(append([]string(nil), declared...), external...), "")
	for i, j := 0, len(declared)-1; i < j; i, j = i+1, j-1 {
		declared[i], declared[j] = declared[j], declared[i]
	}
	for i, j := 0, len(external)-1; i < j; i, j = i+1, j-1 {
		external[i], external[j] = external[j], external[i]
	}
	reversed := strings.Join(append(append([]string(nil), external...), declared...), "")
	discoverer := RelatedSiteDiscoverer{}
	got, err := discoverer.Discover("https://same.example/article", []byte(forward))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	gotReversed, err := discoverer.Discover("https://same.example/article", []byte(reversed))
	if err != nil {
		t.Fatalf("reversed Discover() error = %v", err)
	}
	if len(got) != 30 || got[0].FeedURL != "https://same.example/feed/00" || got[19].FeedURL != "https://same.example/feed/19" || got[20].FeedURL != "https://outside-00.example/path" || got[29].FeedURL != "https://outside-09.example/path" {
		t.Fatalf("bounded related candidates = %#v", got)
	}
	if !reflect.DeepEqual(got, gotReversed) {
		t.Fatal("Related candidates depend on HTML order")
	}
}

func TestRelatedSiteDiscovererRejectsFourMiBPlusOne(t *testing.T) {
	_, err := (RelatedSiteDiscoverer{}).Discover("https://same.example/article", make([]byte, defaultProviderBodyBytes+1))
	if err == nil {
		t.Fatal("Discover() accepted body over four MiB")
	}
}

func TestRedditLinkStreamAdapterAggregatesExternalDomains(t *testing.T) {
	const document = `<rss><channel>
 <item><link>https://www.reddit.com/r/golang/comments/1</link><description><![CDATA[<a href="https://Blog.Example/posts/z?utm_source=r">one</a><img src="https://cdn.example/p.png">]]></description></item>
 <item><link>https://rsshub.app/reddit/subreddit/golang</link><content><![CDATA[See https://blog.example/posts/a and https://other.example/a]]></content></item>
</channel></rss>`
	got, err := (RedditLinkStreamAdapter{}).Parse(Provider{Topic: "golang"}, []byte(document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate count = %d, want 2: %#v", len(got), got)
	}
	if got[0].FeedURL != "https://blog.example/posts/a" || got[0].OccurrenceCount != 2 || got[0].ExternalKey != "blog.example" {
		t.Errorf("blog candidate = %#v", got[0])
	}
	if got[1].FeedURL != "https://other.example/a" || got[1].OccurrenceCount != 1 {
		t.Errorf("other candidate = %#v", got[1])
	}
}

type fakeRelatedSyncStore struct {
	seeds      []string
	provider   RegistryProvider
	candidates []Candidate
	succeeded  bool
	failed     bool
}

func (store *fakeRelatedSyncStore) LoadRelatedSeeds(context.Context, time.Time, int) ([]string, error) {
	return append([]string(nil), store.seeds...), nil
}
func (store *fakeRelatedSyncStore) LoadRelatedProvider(time.Time) (*RegistryProvider, error) {
	provider := store.provider
	return &provider, nil
}
func (store *fakeRelatedSyncStore) UpsertCandidate(_ int, candidate Candidate, _ time.Time) (int, error) {
	store.candidates = append(store.candidates, candidate)
	return len(store.candidates), nil
}
func (store *fakeRelatedSyncStore) RecordSuccess(int, time.Time, string, string) error {
	store.succeeded = true
	return nil
}
func (store *fakeRelatedSyncStore) RecordFailure(int, time.Time, error) error {
	store.failed = true
	return nil
}

type fakeRelatedQueue struct {
	mu         sync.Mutex
	priorities []int
}

func (queue *fakeRelatedQueue) Enqueue(_ int, _ string, priority int) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.priorities = append(queue.priorities, priority)
	return nil
}

func TestRelatedSiteSyncUsesSafeSeedsQueuesCandidatesAndContinuesAfterFailure(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)
	store := &fakeRelatedSyncStore{
		provider: RegistryProvider{ID: 9, Key: "related-sites", Kind: "related_site", Enabled: true},
		seeds:    []string{"https://8.8.8.8/ok", "https://8.8.8.8/fail"},
	}
	queue := &fakeRelatedQueue{}
	client := ProviderClient{
		validateURL: func(raw string) (*url.URL, error) { return url.Parse(raw) },
		publicDoer: relatedRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/fail" {
				return nil, errors.New("temporary")
			}
			body := `<link rel="alternate" type="application/rss+xml" href="/feed.xml"><a href="https://9.9.9.9/blog/post">blog</a>`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}),
	}
	result := (RelatedSiteSync{Store: store, Queue: queue, Client: client}).Sync(context.Background(), now)
	if result.Seeds != 2 || result.Failures != 1 || result.Candidates != 2 || !store.succeeded {
		t.Fatalf("result=%+v store=%+v", result, store)
	}
	if len(queue.priorities) != 2 || queue.priorities[0] != RelatedPriorityDirect || queue.priorities[1] != RelatedPriorityIndirect {
		t.Fatalf("priorities=%v", queue.priorities)
	}
}

type relatedRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn relatedRoundTripFunc) Do(request *http.Request) (*http.Response, error) { return fn(request) }
