package explore

import "testing"

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
