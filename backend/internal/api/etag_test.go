package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
)

func TestComputeListETagStable(t *testing.T) {
	items := []api.ArticleListItem{
		{ID: 1, FetchedAt: time.Unix(100, 0)},
		{ID: 2, FetchedAt: time.Unix(200, 0)},
	}
	a := api.ComputeListETag("k1", items)
	b := api.ComputeListETag("k1", items)
	if a != b {
		t.Fatalf("same input must produce same etag: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, `W/"`) || !strings.HasSuffix(a, `"`) {
		t.Fatalf("expected weak etag format W/\"...\", got %q", a)
	}
}

func TestComputeListETagChangesOnContent(t *testing.T) {
	base := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(100, 0)}}
	tag1 := api.ComputeListETag("k1", base)

	updated := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(999, 0)}}
	if tag1 == api.ComputeListETag("k1", updated) {
		t.Fatalf("etag must change when fetched_at changes")
	}

	if tag1 == api.ComputeListETag("k2", base) {
		t.Fatalf("etag must change when query signature changes")
	}

	base2 := []api.ArticleListItem{
		{ID: 1, FetchedAt: time.Unix(100, 0)},
		{ID: 2, FetchedAt: time.Unix(100, 0)},
	}
	if tag1 == api.ComputeListETag("k1", base2) {
		t.Fatalf("etag must change when item count changes")
	}
	_ = model.UserTag{} // keep import
}

func TestListETagHeaderIsPresent(t *testing.T) {
	items := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(100, 0)}}
	got := api.ComputeListETag("u=1", items)
	if got == "" {
		t.Fatalf("etag must not be empty")
	}
}

func TestComputeDetailETagStable(t *testing.T) {
	art := model.Article{
		ID:              7,
		FetchedAt:       time.Unix(500, 0),
		SummaryDetailed: "abc",
		Content:         "hello world",
	}
	a := api.ComputeDetailETag(art)
	b := api.ComputeDetailETag(art)
	if a != b {
		t.Fatalf("detail etag must be stable: %q vs %q", a, b)
	}
}

func TestComputeDetailETagChangesOnUpdate(t *testing.T) {
	art := model.Article{ID: 7, FetchedAt: time.Unix(500, 0), Content: "v1", SummaryDetailed: "s1"}
	tag1 := api.ComputeDetailETag(art)
	art.Content = "v2"
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when content changes")
	}
	art.Content = "v1"
	art.SummaryDetailed = "s2"
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when summary_detailed changes")
	}
	art.SummaryDetailed = "s1"
	art.FetchedAt = time.Unix(999, 0)
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when fetched_at changes")
	}
}
