package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRedditLinkStreamAdapterBoundsDomainsAndIsOrderIndependent(t *testing.T) {
	build := func(reverse bool) string {
		var document strings.Builder
		document.WriteString(`<rss><channel>`)
		for offset := 0; offset < 2101; offset++ {
			i := offset
			if reverse {
				i = 2100 - offset
			}
			fmt.Fprintf(&document, `<item><description>https://site-%04d.example/post</description></item>`, i)
		}
		document.WriteString(`</channel></rss>`)
		return document.String()
	}

	adapter := RedditLinkStreamAdapter{}
	forward, err := adapter.Parse(Provider{Topic: "programming"}, []byte(build(false)))
	if err != nil {
		t.Fatalf("forward Parse() error = %v", err)
	}
	reversed, err := adapter.Parse(Provider{Topic: "programming"}, []byte(build(true)))
	if err != nil {
		t.Fatalf("reversed Parse() error = %v", err)
	}
	if len(forward) != maxProviderCandidates || forward[0].FeedURL != "https://site-0000.example/post" || forward[len(forward)-1].FeedURL != "https://site-1999.example/post" {
		t.Fatalf("bounded candidates = %d, first/last = %#v / %#v", len(forward), forward[0], forward[len(forward)-1])
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatal("Reddit candidates depend on item order")
	}
}

func TestRedditLinkStreamAdapterCoalescesDomainURLsAndOccurrences(t *testing.T) {
	const document = `<rss><channel>
<item><description>https://example.com/posts/z</description></item>
<item><content>https://example.com/posts/a</content></item>
<item><link>https://example.com/posts/z</link></item>
</channel></rss>`

	got, err := (RedditLinkStreamAdapter{}).Parse(Provider{Topic: "programming"}, []byte(document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(got) != 1 || got[0].FeedURL != "https://example.com/posts/a" || got[0].OccurrenceCount != 3 {
		t.Fatalf("candidate = %#v", got)
	}
}

func TestRedditLinkStreamAdapterRejectsBodyOverFourMiBAndMalformedXML(t *testing.T) {
	adapter := RedditLinkStreamAdapter{}
	overLimit := xmlBodyOverProviderLimit(`<rss><channel>`, `</channel></rss>`)
	if _, err := adapter.Parse(Provider{}, []byte(overLimit)); err == nil {
		t.Fatal("Parse() accepted body over four MiB")
	}
	if _, err := adapter.Parse(Provider{}, []byte(`<rss><channel><item><description>https://example.com/feed</description>`)); err == nil {
		t.Fatal("Parse() accepted malformed XML")
	}
}
