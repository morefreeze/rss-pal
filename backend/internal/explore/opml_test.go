package explore

import "testing"

func TestOPMLRegistryAdapterParsesNestedCategoriesAndDeduplicatesFeeds(t *testing.T) {
	const document = `<?xml version="1.0"?><opml><body>
  <outline text="Technology">
    <outline title="Go Blog" xmlUrl="https://EXAMPLE.com/Feed?utm_source=opml" htmlUrl="https://example.com" />
    <outline text="Languages"><outline text="Rust Blog" xmlUrl="https://rust.example/feed" /></outline>
  </outline>
  <outline text="Copy" xmlUrl="https://example.com/Feed#entry" />
</body></opml>`

	got, err := (OPMLRegistryAdapter{}).Parse(Provider{Topic: "programming"}, []byte(document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate count = %d, want 2: %#v", len(got), got)
	}
	if got[0].FeedURL != "https://example.com/Feed" || got[0].SiteURL != "https://example.com" || got[0].Title != "Go Blog" {
		t.Errorf("first candidate = %#v", got[0])
	}
	if got[0].OccurrenceCount != 2 || !hasAll(got[0].Tags, "programming", "Technology") {
		t.Errorf("first candidate tags/count = %#v", got[0])
	}
	if got[1].Title != "Rust Blog" || !hasAll(got[1].Tags, "programming", "Technology", "Languages") {
		t.Errorf("nested candidate = %#v", got[1])
	}
}

func hasAll(values []string, want ...string) bool {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	for _, value := range want {
		if !set[value] {
			return false
		}
	}
	return true
}
