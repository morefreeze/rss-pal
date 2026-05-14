package backup

import (
	"strings"
	"time"
)

// SavedSnapshotVersion is bumped when the on-disk SavedSnapshot shape changes
// in a way an older reader cannot handle. Independent of SnapshotVersion.
const SavedSnapshotVersion = 1

// savedFileSuffix replaces ".json" on the metadata filename to derive the
// sibling saved-archive path. The chosen suffix encodes both that the file
// is the saved archive AND that it is gzip-compressed.
const savedFileSuffix = ".saved.json.gz"

// SavedArticleRow is one saved article in serializable form.
//
// ExportID is the DB id at backup time. After restore upserts the article we
// record old→new id mapping so article_user_tags / user_preferences /
// reading_progress can be reconnected.
//
// FeedURL is the natural-key reference into the metadata file's feeds list —
// after feed upsert we look up the new feed_id by URL.
type SavedArticleRow struct {
	ExportID             int        `json:"export_id"`
	FeedURL              string     `json:"feed_url"`
	Title                string     `json:"title"`
	URL                  string     `json:"url"`
	Content              string     `json:"content"`
	PublishedAt          *time.Time `json:"published_at,omitempty"`
	SummaryBrief         string     `json:"summary_brief,omitempty"`
	SummaryDetailed      string     `json:"summary_detailed,omitempty"`
	FetchedAt            time.Time  `json:"fetched_at"`
	WordCount            int        `json:"word_count"`
	ReadingMinutes       int        `json:"reading_minutes"`
	IsRead               bool       `json:"is_read"`
	EditorNote           string     `json:"editor_note,omitempty"`
	MediaURL             string     `json:"media_url,omitempty"`
	MediaType            string     `json:"media_type,omitempty"`
	MediaDurationSeconds int        `json:"media_duration_seconds,omitempty"`
}

// ReadingProgressRow attaches to a SavedArticleRow via ArticleExportID.
type ReadingProgressRow struct {
	UserID          int       `json:"user_id"`
	ArticleExportID int       `json:"article_export_id"`
	ScrollPosition  float64   `json:"scroll_position"`
	LastReadAt      time.Time `json:"last_read_at"`
	IsCompleted     bool      `json:"is_completed"`
}

// SavedSnapshot is the on-disk shape of file ② (the saved-archive sibling).
// CreatedAt must match the paired metadata snapshot's CreatedAt — pairing is
// canonically by filename, equality is just a sanity check.
type SavedSnapshot struct {
	Version         int                  `json:"version"`
	CreatedAt       time.Time            `json:"created_at"`
	SavedArticles   []SavedArticleRow    `json:"saved_articles"`
	ReadingProgress []ReadingProgressRow `json:"reading_progress"`
}

// savedSiblingPath returns the path of the saved-archive sibling file for a
// given metadata-file path. Pure string transform — does not stat the disk.
func savedSiblingPath(metadataPath string) string {
	if strings.HasSuffix(metadataPath, fileNameSuffix) {
		return strings.TrimSuffix(metadataPath, fileNameSuffix) + savedFileSuffix
	}
	// Fallback: append. Shouldn't happen if caller passed a real metadata path.
	return metadataPath + savedFileSuffix
}
