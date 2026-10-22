package sourcedirectory

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixture = `{"sample":{"name":"Sample","url":"example.com","heat":15,"categories":["news"],"ownerUserId":"secret-owner","routes":{"/sample/user/:id":{"path":["/user/:id","/profile/:id"],"name":"Profile","categories":["social-media","news"],"description":"<b>text</b>","example":"/sample/user/123","parameters":{"id":"User ID","kind":{"description":"Kind","default":2,"options":[{"label":"Two","value":2}]}},"features":{"requireConfig":[{"name":"SAMPLE_COOKIE","description":"private"}],"antiCrawler":true},"topFeeds":[{"title":"Public","url":"rsshub://sample/user/123","ownerUserId":null,"errorMessage":"secret-error"},{"title":"Private user","url":"https://example.com/private-rss","ownerUserId":"private-user"},{"title":"Private","url":"http://127.0.0.1/feed"},{"title":"Password","url":"https://user:password@example.com/rss"},{"title":"Token","url":"https://example.com/rss?token=secret"}]}}}}`

func TestNormalizeWhitelistAndCompleteness(t *testing.T) {
	d, err := Normalize([]byte(fixture), time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if d.PlatformCount != 1 || d.RouteCount != 1 {
		t.Fatalf("counts: %+v", d)
	}
	p := d.Platforms[0]
	r := p.Routes[0]
	if p.ID != "sample" || p.URL != "https://example.com" || len(p.Categories) != 2 || len(r.Categories) != 2 {
		t.Fatalf("platform/categories: %+v", p)
	}
	if r.ID != "sample:/sample/user/:id" || r.Path != "/user/:id" || !r.RequiresConfig || !r.AntiCrawler || len(r.ConfigNames) != 1 {
		t.Fatalf("route: %+v", r)
	}
	if r.Parameters["kind"].Default != "2" || r.Parameters["kind"].Options[0].Value != "2" {
		t.Fatal(r.Parameters)
	}
	if len(r.TopFeeds) != 1 || r.TopFeeds[0].URL != "rsshub://sample/user/123" {
		t.Fatal(r.TopFeeds)
	}
	b, _ := json.Marshal(d)
	for _, private := range []string{"secret-owner", "ownerUserId", "secret-error", "errorMessage", "password", "token=secret", "maintainers", "private-rss"} {
		if strings.Contains(string(b), private) {
			t.Fatalf("leaked %s", private)
		}
	}
}

func TestEmbeddedSnapshotComplete(t *testing.T) {
	d, err := embedded()
	if err != nil {
		t.Fatal(err)
	}
	if d.PlatformCount < 2067 || d.RouteCount < 3955 || len(d.Platforms) != d.PlatformCount {
		t.Fatalf("incomplete snapshot %d platforms %d routes", d.PlatformCount, d.RouteCount)
	}
	seen := map[string]bool{}
	count := 0
	for _, p := range d.Platforms {
		for _, r := range p.Routes {
			if seen[r.ID] {
				t.Fatalf("duplicate %s", r.ID)
			}
			seen[r.ID] = true
			count++
		}
	}
	if count != d.RouteCount {
		t.Fatal(count, d.RouteCount)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshFailureRetainsSnapshotAndBoundsFetch(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		length     int64
	}{{"invalid", `{}`, 200, 2}, {"large", strings.Repeat(" ", int(maxBytes)+1), 200, -1}, {"status", `error`, 500, 5}} {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			before := s.directory
			s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != SourceURL {
					t.Fatal(r.URL)
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Error("missing fetch deadline")
				}
				return &http.Response{StatusCode: tc.status, ContentLength: tc.length, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			if err := s.refresh(); err == nil {
				t.Fatal("expected rejection")
			}
			after := s.directory
			if after.RouteCount != before.RouteCount {
				t.Fatal("lost fallback")
			}
		})
	}
}
func TestRefreshAcceptsCompleteDocument(t *testing.T) {
	s := New()
	s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})}
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	d := s.Get()
	if d.PlatformCount != 1 || d.Stale {
		t.Fatalf("refresh failed: %+v", d)
	}
}
func TestSafePublicURLs(t *testing.T) {
	for _, u := range []string{"http://localhost/a", "https://192.168.1.1/a", "https://[::1]/a", "file:///etc/passwd", "https://u:p@example.com", "rsshub://sample/a?token=x", "rsshub://127.0.0.1/a", "https://example.internal/a", "https://foo.local/a", "https://example.com/a?access_token=x"} {
		if safeURL(u, true) != "" {
			t.Errorf("accepted %s", u)
		}
	}
}

// This optional archived-input check proves every upstream entry and alias was
// retained without checking a 9 MiB private-field-rich raw response into Git.
func TestArchivedUpstreamParity(t *testing.T) {
	path := os.Getenv("SOURCE_DIRECTORY_UPSTREAM")
	if path == "" {
		t.Skip("set SOURCE_DIRECTORY_UPSTREAM to an archived routes.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var upstream map[string]upstreamPlatform
	if err = json.Unmarshal(raw, &upstream); err != nil {
		t.Fatal(err)
	}
	d, err := Normalize(raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(upstream) != d.PlatformCount {
		t.Fatal("platform omission")
	}
	actual := map[string]Route{}
	for _, p := range d.Platforms {
		for _, r := range p.Routes {
			actual[r.ID] = r
		}
	}
	count := 0
	for ns, p := range upstream {
		for key, r := range p.Routes {
			count++
			out, ok := actual[ns+":"+key]
			if !ok {
				t.Fatalf("missing %s %s", ns, key)
			}
			var paths []string
			var path string
			if json.Unmarshal(r.Path, &path) == nil {
				paths = []string{path}
			} else if err = json.Unmarshal(r.Path, &paths); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(paths, out.Paths) {
				t.Fatalf("lost aliases %s", out.ID)
			}
		}
	}
	if count != d.RouteCount {
		t.Fatal(count, d.RouteCount)
	}
	embeddedDirectory, err := embedded()
	if err != nil {
		t.Fatal(err)
	}
	// The generated snapshot must exactly match the shared normalizer for its
	// recorded fetch timestamp, including fields, sorting and every route key.
	timestamp, err := time.Parse(time.RFC3339, embeddedDirectory.FetchedAt)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := Normalize(raw, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, embeddedDirectory) {
		t.Fatal("snapshot differs from deterministic normalization")
	}
}

func TestCategoryFallbackPreservesOverridesAndAliasPaths(t *testing.T) {
	d, err := Normalize([]byte(`{"a":{"name":"A","categories":["university"],"routes":{"/a/one":{"path":["/one","/alias"],"categories":["programming","study"]},"/a/two":{"path":"/two"}}}}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	routes := d.Platforms[0].Routes
	if !reflect.DeepEqual(routes[0].Categories, []string{"programming", "study"}) || !reflect.DeepEqual(routes[1].Categories, []string{"university"}) {
		t.Fatal(routes)
	}
	if !reflect.DeepEqual(routes[0].Paths, []string{"/one", "/alias"}) {
		t.Fatal(routes[0].Paths)
	}
}

func TestExpiredCacheReturnsImmediatelyAndCoalescesRefresh(t *testing.T) {
	s := New()
	s.directory.FetchedAt = time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return nil, errors.New("upstream unavailable")
	})}
	for i := 0; i < 50; i++ {
		d := s.Get()
		if !d.Stale || d.RouteCount < 3955 {
			t.Fatal("expired snapshot not served")
		}
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh not started")
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate refresh")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		finished := !s.refreshing
		s.mu.Unlock()
		if finished {
			d := s.Get()
			if !d.Stale || d.RouteCount < 3955 || calls.Load() != 1 {
				t.Fatal("fallback or retry throttle failed")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("refresh did not finish")
}

func TestUnsafeRouteExamplesExcluded(t *testing.T) {
	for _, example := range []string{"/sample/user/123?token=private", "/sample/user/123?api_key=private", "https://u:p@example.com/rss", "javascript:alert(1)", "//127.0.0.1/private"} {
		input := strings.Replace(fixture, "/sample/user/123\",\"parameters", example+"\",\"parameters", 1)
		d, err := Normalize([]byte(input), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if d.Platforms[0].Routes[0].Example != "" {
			t.Errorf("unsafe example retained: %s", example)
		}
	}
}

func TestArchivedFoloCategoryParity(t *testing.T) {
	path := os.Getenv("SOURCE_DIRECTORY_UPSTREAM")
	if path == "" {
		t.Skip("set SOURCE_DIRECTORY_UPSTREAM to an archived routes.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var upstream map[string]upstreamPlatform
	if err = json.Unmarshal(raw, &upstream); err != nil {
		t.Fatal(err)
	}
	d, err := embedded()
	if err != nil {
		t.Fatal(err)
	}
	categories := []string{"social-media", "new-media", "traditional-media", "bbs", "blog", "programming", "design", "live", "multimedia", "picture", "anime", "program-update", "shopping", "game", "reading", "journal", "finance"}
	unionPlatforms := map[string]bool{}
	unionRoutes := map[string]bool{}
	for _, category := range categories {
		expected := map[string]bool{}
		expectedPlatforms := map[string]bool{}
		for ns, p := range upstream {
			for key, r := range p.Routes {
				effective := r.Categories
				if len(effective) == 0 {
					effective = p.Categories
				}
				for _, c := range effective {
					if c == category {
						expected[ns+":"+key] = true
						expectedPlatforms[ns] = true
						unionPlatforms[ns] = true
						unionRoutes[ns+":"+key] = true
					}
				}
			}
		}
		actual := map[string]bool{}
		platforms := map[string]bool{}
		for _, p := range d.Platforms {
			for _, r := range p.Routes {
				for _, c := range r.Categories {
					if c == category {
						actual[r.ID] = true
						platforms[p.ID] = true
					}
				}
			}
		}
		if !reflect.DeepEqual(expected, actual) || !reflect.DeepEqual(expectedPlatforms, platforms) {
			t.Fatalf("category %s mismatch", category)
		}
		t.Logf("%s: %d platforms / %d routes", category, len(platforms), len(actual))
	}
	t.Logf("17-category union: %d platforms / %d routes", len(unionPlatforms), len(unionRoutes))
}
