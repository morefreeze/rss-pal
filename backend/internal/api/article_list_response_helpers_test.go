package api_test

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
)

// newArticleListItemForTest returns a zero-valued list item with the
// non-omitempty required fields populated, so the marshal-shape test
// can rely on stable key presence.
func newArticleListItemForTest() api.ArticleListItem {
	return api.ArticleListItem{
		ID:           1,
		FeedID:       2,
		Title:        "t",
		URL:          "u",
		SummaryBrief: "s",
		ManualTags:   []model.UserTag{},
	}
}
