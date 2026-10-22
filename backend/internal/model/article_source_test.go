package model

import "testing"

func TestExtractedArticleSourceUsesOriginalWebsite(t *testing.T) {
	parent := 3248
	for _, raw := range []string{"https://deskmind.dev/zh/blog/launch", "https://WWW.DeskMind.dev:443/zh/blog/launch"} {
		a := Article{FeedID: 23, FeedTitle: "阮一峰的网络日志", URL: raw, ParentArticleID: &parent}
		a.ResolveSource()
		if a.Source == nil || a.Source.Title != "deskmind.dev" || a.Source.Key != "host:deskmind.dev" || a.Source.URL != raw {
			t.Fatalf("wrong original source: %+v", a.Source)
		}
		if a.FeedID != 23 || a.FeedTitle != "阮一峰的网络日志" {
			t.Fatal("lost discovery feed")
		}
	}
}

func TestArticleSourceLeavesNormalFeedsAndInvalidLinksAlone(t *testing.T) {
	parent := 3248
	for _, a := range []Article{
		{FeedID: 23, FeedTitle: "阮一峰的网络日志", URL: "https://ruanyifeng.com/blog"},
		{URL: "javascript:alert(1)", ParentArticleID: &parent},
		{URL: "https:///broken", ParentArticleID: &parent},
	} {
		a.ResolveSource()
		if a.Source != nil {
			t.Fatalf("unexpected host source: %+v", a.Source)
		}
	}
}
