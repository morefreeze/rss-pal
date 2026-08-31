package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
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
