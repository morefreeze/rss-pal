package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDirectoryAdapterUsesExternalEntryLinksNotDirectoryFeed(t *testing.T) {
	const document = `<feed xmlns="http://www.w3.org/2005/Atom">
 <entry><title>Alpha</title><category term="engineering"/><link rel="self" href="https://ooh.directory/feeds/alpha.xml"/><link rel="alternate" href="https://alpha.example/blog"/></entry>
 <entry><title>Directory mirror</title><link href="https://ooh.directory/feeds/mirror.xml"/></entry>
</feed>`
	provider := Provider{Endpoint: "https://ooh.directory/feeds/recently-added.xml", Topic: "recent"}
	got, err := (DirectoryAdapter{}).Parse(provider, []byte(document))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(got), got)
	}
	if got[0].FeedURL != "https://alpha.example/blog" || got[0].Title != "Alpha" || !hasAll(got[0].Tags, "engineering", "recent") {
		t.Errorf("candidate = %#v", got[0])
	}
}

func TestDirectoryAdapterParsesRSSItems(t *testing.T) {
	const document = `<rss><channel><item><title>Beta</title><category>news</category><link>https://beta.example/</link></item></channel></rss>`
	got, err := (DirectoryAdapter{}).Parse(Provider{Endpoint: "https://directory.example/feed.xml", Topic: "recent"}, []byte(document))
	if err != nil || len(got) != 1 {
		t.Fatalf("candidates=%#v err=%v", got, err)
	}
	if got[0].FeedURL != "https://beta.example/" || !hasAll(got[0].Tags, "recent", "news") {
		t.Errorf("candidate=%#v", got[0])
	}
}

func TestDirectoryAdapterKeepsLexicallySmallestCandidatesRegardlessOfEntryOrder(t *testing.T) {
	build := func(reverse bool) string {
		var document strings.Builder
		document.WriteString(`<rss><channel>`)
		for offset := 0; offset < 2100; offset++ {
			i := offset
			if reverse {
				i = 2099 - offset
			}
			fmt.Fprintf(&document, `<item><title>Feed %04d</title><category>news</category><link>https://feeds.example/%04d?utm_source=directory</link></item>`, i, i)
		}
		document.WriteString(`</channel></rss>`)
		return document.String()
	}
	provider := Provider{Endpoint: "https://directory.example/feed.xml", Topic: "recent"}
	forward, err := (DirectoryAdapter{}).Parse(provider, []byte(build(false)))
	if err != nil {
		t.Fatalf("forward Parse() error = %v", err)
	}
	reversed, err := (DirectoryAdapter{}).Parse(provider, []byte(build(true)))
	if err != nil {
		t.Fatalf("reversed Parse() error = %v", err)
	}
	if len(forward) != maxProviderCandidates || forward[0].FeedURL != "https://feeds.example/0000" || forward[len(forward)-1].FeedURL != "https://feeds.example/1999" {
		t.Fatalf("forward result = %d, first/last = %#v / %#v", len(forward), forward[0], forward[len(forward)-1])
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatal("Directory direct adapter result depends on entry order")
	}
}

func TestXMLAdaptersRejectBodyOverFourMiB(t *testing.T) {
	tests := []struct {
		name    string
		adapter ProviderAdapter
		body    string
	}{
		{name: "OPML", adapter: OPMLRegistryAdapter{}, body: xmlBodyOverProviderLimit(`<opml><body>`, `</body></opml>`)},
		{name: "Directory", adapter: DirectoryAdapter{}, body: xmlBodyOverProviderLimit(`<rss><channel>`, `</channel></rss>`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.body) != defaultProviderBodyBytes+1 {
				t.Fatalf("body length = %d, want %d", len(test.body), defaultProviderBodyBytes+1)
			}
			_, err := test.adapter.Parse(Provider{}, []byte(test.body))
			if err == nil || !strings.Contains(err.Error(), "provider body exceeds") {
				t.Fatalf("Parse() error = %v, want provider body limit", err)
			}
		})
	}
}

func xmlBodyOverProviderLimit(prefix, suffix string) string {
	return prefix + strings.Repeat(" ", defaultProviderBodyBytes+1-len(prefix)-len(suffix)) + suffix
}

func TestXMLAdaptersRejectMalformedDocuments(t *testing.T) {
	tests := []struct {
		name    string
		adapter ProviderAdapter
		body    string
	}{
		{name: "OPML", adapter: OPMLRegistryAdapter{}, body: `<opml><body><outline xmlUrl="https://feeds.example/a">`},
		{name: "Directory", adapter: DirectoryAdapter{}, body: `<rss><channel><item><link>https://feeds.example/a</link>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.adapter.Parse(Provider{}, []byte(test.body)); err == nil {
				t.Fatal("Parse() accepted malformed XML")
			}
		})
	}
}
