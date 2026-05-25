package api

import (
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// ArticleListItem is the lean DTO returned by GET /api/articles. It
// deliberately excludes the heavy `content` and `summary_detailed`
// fields — the list UI only renders the brief summary. Detail pages
// fetch the full article via GET /api/articles/:id.
type ArticleListItem struct {
	ID                   int             `json:"id"`
	FeedID               int             `json:"feed_id"`
	FeedTitle            string          `json:"feed_title,omitempty"`
	Title                string          `json:"title"`
	URL                  string          `json:"url"`
	PublishedAt          *time.Time      `json:"published_at"`
	SummaryBrief         string          `json:"summary_brief"`
	FetchedAt            time.Time       `json:"fetched_at"`
	WordCount            int             `json:"word_count"`
	ReadingMinutes       int             `json:"reading_minutes"`
	IsRead               bool            `json:"is_read"`
	MediaURL             string          `json:"media_url,omitempty"`
	MediaType            string          `json:"media_type,omitempty"`
	MediaDurationSeconds int             `json:"media_duration_seconds,omitempty"`
	LinksExtendable      *bool           `json:"links_extendable,omitempty"`
	LinkSetSuggested     *bool           `json:"link_set_suggested,omitempty"`
	ParentArticleID      *int            `json:"parent_article_id,omitempty"`
	ProcessingState      string          `json:"processing_state,omitempty"`
	PrerankScore         *float64        `json:"prerank_score,omitempty"`
	EditorNote           string          `json:"editor_note,omitempty"`
	ManualTags           []model.UserTag `json:"manual_tags"`
}

// articleToListItem projects a model.Article onto the lean DTO.
func articleToListItem(a model.Article, tags []model.UserTag) ArticleListItem {
	if tags == nil {
		tags = []model.UserTag{}
	}
	return ArticleListItem{
		ID:                   a.ID,
		FeedID:               a.FeedID,
		FeedTitle:            a.FeedTitle,
		Title:                a.Title,
		URL:                  a.URL,
		PublishedAt:          a.PublishedAt,
		SummaryBrief:         a.SummaryBrief,
		FetchedAt:            a.FetchedAt,
		WordCount:            a.WordCount,
		ReadingMinutes:       a.ReadingMinutes,
		IsRead:               a.IsRead,
		MediaURL:             a.MediaURL,
		MediaType:            a.MediaType,
		MediaDurationSeconds: a.MediaDurationSeconds,
		LinksExtendable:      a.LinksExtendable,
		LinkSetSuggested:     a.LinkSetSuggested,
		ParentArticleID:      a.ParentArticleID,
		ProcessingState:      a.ProcessingState,
		PrerankScore:         a.PrerankScore,
		EditorNote:           a.EditorNote,
		ManualTags:           tags,
	}
}
