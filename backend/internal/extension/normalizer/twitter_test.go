package normalizer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestTwitterNormalizer_TextOnly(t *testing.T) {
	n := NewTwitterNormalizer()
	feed := &model.Feed{ID: 42}
	raw, _ := json.Marshal(TweetItem{
		ID:          "12345",
		Author:      "karpathy",
		DisplayName: "Andrej Karpathy",
		Text:        "Hello world",
		CreatedAt:   time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		URL:         "https://x.com/karpathy/status/12345",
	})

	art, err := n.Normalize(raw, feed)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if art.FeedID != 42 {
		t.Errorf("FeedID = %d, want 42", art.FeedID)
	}
	if art.Kind != "tweet" {
		t.Errorf("Kind = %q, want tweet", art.Kind)
	}
	if art.URL != "https://x.com/karpathy/status/12345" {
		t.Errorf("URL = %q", art.URL)
	}
	if !strings.Contains(art.Title, "Hello world") {
		t.Errorf("Title = %q, want to contain text", art.Title)
	}
	if !strings.Contains(art.Content, "@karpathy") {
		t.Errorf("Content should contain handle: %s", art.Content)
	}
	if !art.IsClip {
		t.Error("IsClip should be true (consistent with bookmarklet path)")
	}
	if art.PublishedAt == nil || !art.PublishedAt.Equal(time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("PublishedAt = %v", art.PublishedAt)
	}
}

func TestTwitterNormalizer_WithImagesAndQuote(t *testing.T) {
	n := NewTwitterNormalizer()
	raw, _ := json.Marshal(TweetItem{
		ID:        "1",
		Author:    "x",
		Text:      "body",
		URL:       "https://x.com/x/status/1",
		MediaURLs: []string{"https://pbs.twimg.com/media/a.jpg"},
		QuotedURL: "https://x.com/y/status/9",
	})
	art, _ := n.Normalize(raw, &model.Feed{ID: 1})
	if !strings.Contains(art.Content, "https://pbs.twimg.com/media/a.jpg") {
		t.Errorf("missing image in content")
	}
	if !strings.Contains(art.Content, "https://x.com/y/status/9") {
		t.Errorf("missing quote in content")
	}
}

func TestTwitterNormalizer_PrefixMatchesAllTwitterKinds(t *testing.T) {
	n := NewTwitterNormalizer()
	if !strings.HasPrefix("twitter:list", n.SourceKindPrefix()) {
		t.Errorf("SourceKindPrefix should match twitter:list")
	}
	if !strings.HasPrefix("twitter:bookmarks", n.SourceKindPrefix()) {
		t.Errorf("SourceKindPrefix should match twitter:bookmarks")
	}
}
