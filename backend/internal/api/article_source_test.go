package api

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestListItemPreservesOriginalLinkSource(t *testing.T) {
	parent := 3248
	a := model.Article{ID: 3249, FeedID: 23, FeedTitle: "阮一峰的网络日志", URL: "https://deskmind.dev/zh/blog/launch", ParentArticleID: &parent}
	got := articleToListItem(a, nil)
	if got.Source == nil || got.Source.Title != "deskmind.dev" {
		t.Fatalf("list source=%+v", got.Source)
	}
}

func TestListETagChangesWhenExtractedSourceChanges(t *testing.T) {
	parent := 3248
	a := model.Article{ID: 3249, FeedID: 23, FeedTitle: "阮一峰的网络日志", URL: "https://deskmind.dev/zh/blog/launch", ParentArticleID: &parent}
	before := ComputeListETag("", []ArticleListItem{articleToListItem(a, nil)})
	a.URL = "https://original.example/blog"
	after := ComputeListETag("", []ArticleListItem{articleToListItem(a, nil)})
	if before == after {
		t.Fatal("source repair must invalidate cached attribution")
	}
}
