package backup

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
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

// WriteSavedFile serializes ss as gzipped JSON to the sibling path derived
// from metadataPath. Atomic via tmp + rename.
func WriteSavedFile(ss *SavedSnapshot, metadataPath string) error {
	savedPath := savedSiblingPath(metadataPath)
	tmp := savedPath + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	gz := gzip.NewWriter(f)
	enc := json.NewEncoder(gz)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ss); err != nil {
		gz.Close()
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encode saved snapshot: %w", err)
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flush gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, savedPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, savedPath, err)
	}
	return nil
}

// LoadSaved reads the sibling saved-archive for metadataPath. Returns
// (nil, nil) if the sibling doesn't exist (legacy backup, or saved write
// didn't complete — either way, callers fall back to metadata-only restore).
// Returns an error only for non-IsNotExist conditions (corrupted gzip,
// malformed JSON, unsupported version).
func LoadSaved(metadataPath string) (*SavedSnapshot, error) {
	savedPath := savedSiblingPath(metadataPath)
	f, err := os.Open(savedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", savedPath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gunzip %s: %w", savedPath, err)
	}
	defer gz.Close()

	var ss SavedSnapshot
	if err := json.NewDecoder(gz).Decode(&ss); err != nil {
		return nil, fmt.Errorf("parse %s: %w", savedPath, err)
	}
	if ss.Version > SavedSnapshotVersion {
		return nil, fmt.Errorf("saved snapshot %s has version %d, this build supports %d",
			savedPath, ss.Version, SavedSnapshotVersion)
	}
	return &ss, nil
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
