package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
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

func detailResponse(enabled bool, childState string, progress float64, saved int, hidden bool) gin.H {
	return gin.H{
		"article": model.Article{
			ID:              7,
			Content:         "body",
			FetchedAt:       time.Unix(500, 0),
			LinksExtendable: &enabled,
		},
		"children": []model.Article{{ID: 8, ProcessingState: childState}},
		"progress": gin.H{"scroll_position": progress},
		"signals":  gin.H{"save": saved},
		"hidden":   hidden,
	}
}

func detailTag(t *testing.T, response gin.H) (string, string) {
	t.Helper()
	body, tag, err := api.MarshalDetailResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), tag
}

func TestMarshalDetailResponseStable(t *testing.T) {
	body1, tag1 := detailTag(t, detailResponse(false, "processing", 0.2, 1, false))
	body2, tag2 := detailTag(t, detailResponse(false, "processing", 0.2, 1, false))
	if body1 != body2 || tag1 != tag2 {
		t.Fatalf("detail response must be stable: %q/%q vs %q/%q", body1, tag1, body2, tag2)
	}
	if !strings.HasPrefix(tag1, `W/"`) || !strings.HasSuffix(tag1, `"`) {
		t.Fatalf("expected weak etag, got %q", tag1)
	}
}

func TestMarshalDetailResponseETagCoversCompleteRepresentation(t *testing.T) {
	_, base := detailTag(t, detailResponse(false, "processing", 0.2, 1, false))
	tests := []struct {
		name     string
		response gin.H
	}{
		{name: "link flag", response: detailResponse(true, "processing", 0.2, 1, false)},
		{name: "child state", response: detailResponse(false, "ready", 0.2, 1, false)},
		{name: "progress", response: detailResponse(false, "processing", 0.8, 1, false)},
		{name: "signals", response: detailResponse(false, "processing", 0.2, 0, false)},
		{name: "hidden", response: detailResponse(false, "processing", 0.2, 1, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := detailTag(t, tt.response)
			if got == base {
				t.Fatalf("etag did not change for %s", tt.name)
			}
		})
	}
}
