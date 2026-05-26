package normalizer

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/rss"
)

// TwitterNormalizer handles all twitter:* source kinds — same item shape,
// different feeds.
type TwitterNormalizer struct{}

func NewTwitterNormalizer() *TwitterNormalizer { return &TwitterNormalizer{} }

func (n *TwitterNormalizer) SourceKindPrefix() string { return "twitter:" }

func (n *TwitterNormalizer) Normalize(raw json.RawMessage, feed *model.Feed) (*model.Article, error) {
	var item TweetItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode tweet: %w", err)
	}
	if item.URL == "" || item.ID == "" {
		return nil, fmt.Errorf("tweet missing url or id")
	}

	// Reuse the same builders as the bookmarklet path. Convert TweetItem
	// to TweetCapture so we share BuildTweet{Title,Content} verbatim.
	cap := &rss.TweetCapture{
		Author:       item.Author,
		DisplayName:  item.DisplayName,
		PublishedAt:  item.CreatedAt,
		TextMarkdown: item.Text,
		ImageURLs:    item.MediaURLs,
	}
	if item.QuotedURL != "" {
		cap.Quote = &rss.Quote{URL: item.QuotedURL}
	}

	title := rss.BuildTweetTitle(cap)
	content := rss.BuildTweetContent(cap)
	wordCount, readingMinutes := rss.ComputeMetrics(content)

	var pa *time.Time
	if !item.CreatedAt.IsZero() {
		t := item.CreatedAt
		pa = &t
	}

	return &model.Article{
		FeedID:         feed.ID,
		Title:          title,
		URL:            item.URL,
		Content:        content,
		PublishedAt:    pa,
		IsClip:         true,
		Kind:           "tweet",
		WordCount:      wordCount,
		ReadingMinutes: readingMinutes,
	}, nil
}
