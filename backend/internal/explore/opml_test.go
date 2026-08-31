package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestOPMLRegistryAdapterParsesNestedCategoriesAndDeduplicatesFeeds(t *testing.T) {
	const document = `<?xml version="1.0"?><opml><body>
  <outline text="Technology">
    <outline title="Go Blog" xmlUrl="https://EXAMPLE.com/Feed?utm_source=opml" htmlUrl="https://example.com" category="golang, blogs" />
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
	if got[0].OccurrenceCount != 2 || !hasAll(got[0].Tags, "programming", "Technology", "golang", "blogs") {
		t.Errorf("first candidate tags/count = %#v", got[0])
	}
	if got[1].Title != "Rust Blog" || !hasAll(got[1].Tags, "programming", "Technology", "Languages") {
		t.Errorf("nested candidate = %#v", got[1])
	}
}

func TestOPMLRegistryAdapterKeepsLexicallySmallestFeedsRegardlessOfOutlineOrder(t *testing.T) {
	build := func(reverse bool) string {
		var document strings.Builder
		document.WriteString(`<opml><body><outline text="Technology">`)
		for offset := 0; offset < 2100; offset++ {
			i := offset
			if reverse {
				i = 2099 - offset
			}
			fmt.Fprintf(&document, `<outline text="Feed %04d" xmlUrl="https://feeds.example/%04d?utm_source=registry"/>`, i, i)
		}
		document.WriteString(`</outline></body></opml>`)
		return document.String()
	}
	provider := Provider{Topic: "programming"}
	forward, err := (OPMLRegistryAdapter{}).Parse(provider, []byte(build(false)))
	if err != nil {
		t.Fatalf("forward Parse() error = %v", err)
	}
	reversed, err := (OPMLRegistryAdapter{}).Parse(provider, []byte(build(true)))
	if err != nil {
		t.Fatalf("reversed Parse() error = %v", err)
	}
	if len(forward) != maxProviderCandidates || forward[0].FeedURL != "https://feeds.example/0000" || forward[len(forward)-1].FeedURL != "https://feeds.example/1999" {
		t.Fatalf("forward result = %d, first/last = %#v / %#v", len(forward), forward[0], forward[len(forward)-1])
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatal("OPML direct adapter result depends on outline order")
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
