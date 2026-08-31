package explore

import "testing"

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
	if got[1].FeedURL != "https://blog.example/post" || got[1].ExternalKey != "blog.example" {
		t.Errorf("external site = %#v", got[1])
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
