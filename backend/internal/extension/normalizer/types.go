// Package normalizer turns per-site adapter payloads from the rss-pal
// browser extension into rss-pal Articles ready for repository.Create.
//
// Each source kind (twitter:list / twitter:user / twitter:bookmarks /
// future xhs / weread / ...) has its own normalizer. The extension ingest
// handler picks the normalizer by source_kind prefix.
package normalizer

import (
	"encoding/json"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// IngestRequest is the JSON body of POST /api/extension/ingest.
type IngestRequest struct {
	SourceKind string            `json:"source_kind"` // e.g. "twitter:list"
	SourceID   string            `json:"source_id"`   // list id / handle / "self"
	SourceName string            `json:"source_name"` // human display label
	Items      []json.RawMessage `json:"items"`
}

// IngestResponse is what the handler returns.
type IngestResponse struct {
	Accepted int      `json:"accepted"` // newly created articles
	Skipped  int      `json:"skipped"`  // duplicates (already had this URL)
	Errors   []string `json:"errors"`   // per-item error strings (truncated)
	FeedID   int      `json:"feed_id,omitempty"`   // newly-created or existing feed id for this source
	FeedName string   `json:"feed_name,omitempty"` // human-readable feed title (for popup display)
}

// Normalizer turns one adapter-emitted item into an Article.
type Normalizer interface {
	// SourceKindPrefix returns the prefix this normalizer handles, e.g. "twitter:".
	SourceKindPrefix() string

	// Normalize decodes a raw item and returns an Article ready to Create.
	// The feed argument is the destination feed (already upserted by handler).
	Normalize(raw json.RawMessage, feed *model.Feed) (*model.Article, error)
}

// TweetItem is the shape each twitter adapter emits per tweet.
// Fields match the names emitted by extension/adapters/twitter/*.js.
type TweetItem struct {
	ID          string    `json:"id"`     // numeric tweet id, used for dedupe at adapter layer
	Author      string    `json:"author"` // handle, lowercased
	DisplayName string    `json:"display_name"`
	Text        string    `json:"text"`       // tweet text (already markdown-ish)
	CreatedAt   time.Time `json:"created_at"` // RFC3339 from <time datetime=...>
	URL         string    `json:"url"`        // https://x.com/<user>/status/<id>
	MediaURLs   []string  `json:"media_urls,omitempty"`
	QuotedURL   string    `json:"quoted_url,omitempty"`
	Likes       int       `json:"likes,omitempty"`
	Retweets    int       `json:"retweets,omitempty"`
	Replies     int       `json:"replies,omitempty"`
	Views       int       `json:"views,omitempty"`
}
