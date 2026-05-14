// Package backup snapshots subscription-side state to JSON files on disk and
// applies a tiered retention policy. Articles are intentionally excluded —
// they are re-fetched by the worker from the feed URLs.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// SnapshotVersion is bumped when the on-disk JSON shape changes in a way
// that older readers cannot handle.
const SnapshotVersion = 1

// fileNameLayout is the time.Parse layout for filenames. Seconds are included
// so two backups in the same minute (e.g. rapid add/delete) don't collide.
const (
	fileNamePrefix = "rss-pal-backup-"
	fileNameSuffix = ".json"
	fileTimeLayout = "20060102-150405"
)

// ArticleUserTagRow is the on-disk shape of the article_user_tags join table.
// It deliberately doesn't import a model type because there isn't one — the
// join table is referenced directly via SQL in the existing repository.
type ArticleUserTagRow struct {
	ArticleID int       `json:"article_id"`
	TagID     int       `json:"tag_id"`
	UserID    int       `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Snapshot is the on-disk JSON shape. Fields are pointers/slices of model
// types so JSON round-trips cleanly through the same structs the rest of the
// codebase uses.
type Snapshot struct {
	Version            int                       `json:"version"`
	CreatedAt          time.Time                 `json:"created_at"`
	Feeds              []model.Feed              `json:"feeds"`
	InterestCategories []model.InterestCategory  `json:"interest_categories"`
	InterestTopics     []model.InterestTopic     `json:"interest_topics"`
	UserTags           []model.UserTag           `json:"user_tags"`
	ArticleUserTags    []ArticleUserTagRow       `json:"article_user_tags"`
	UserPreferences    []model.UserPreference    `json:"user_preferences"`
}

// FileInfo is the metadata of a backup file on disk, exposed by List.
type FileInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
}

// Build snapshots both files in one read-only transaction so they are a
// consistent point-in-time view of the DB. The two returned snapshots share
// the same CreatedAt.
func Build(ctx context.Context, db *sql.DB) (*Snapshot, *SavedSnapshot, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	createdAt := time.Now().UTC()
	s := &Snapshot{
		Version:   SnapshotVersion,
		CreatedAt: createdAt,
	}

	if s.Feeds, err = loadFeeds(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load feeds: %w", err)
	}
	if s.InterestCategories, err = loadInterestCategories(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load interest_categories: %w", err)
	}
	if s.InterestTopics, err = loadInterestTopics(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load interest_topics: %w", err)
	}
	if s.UserTags, err = loadUserTags(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load user_tags: %w", err)
	}
	if s.ArticleUserTags, err = loadArticleUserTags(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load article_user_tags: %w", err)
	}
	if s.UserPreferences, err = loadUserPreferences(ctx, tx); err != nil {
		return nil, nil, fmt.Errorf("load user_preferences: %w", err)
	}

	ss, err := buildSaved(ctx, tx, createdAt)
	if err != nil {
		return nil, nil, fmt.Errorf("build saved: %w", err)
	}
	return s, ss, nil
}

func loadFeeds(ctx context.Context, tx *sql.Tx) ([]model.Feed, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, url, COALESCE(title, ''), last_fetched_at, fetch_interval_minutes,
		       COALESCE(etag, ''), COALESCE(last_modified, ''), is_active, owner_id,
		       COALESCE(feed_type, 'rss'), COALESCE(status, 'active'), priority_weight, created_at
		FROM feeds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Feed
	for rows.Next() {
		var f model.Feed
		var ownerID sql.NullInt64
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.LastFetchedAt, &f.FetchIntervalMin,
			&f.ETag, &f.LastModified, &f.IsActive, &ownerID,
			&f.FeedType, &f.Status, &f.PriorityWeight, &f.CreatedAt); err != nil {
			return nil, err
		}
		if ownerID.Valid {
			oid := int(ownerID.Int64)
			f.OwnerID = &oid
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func loadInterestCategories(ctx context.Context, tx *sql.Tx) ([]model.InterestCategory, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, user_id, category, weight, last_reinforced_at
		FROM interest_categories ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.InterestCategory
	for rows.Next() {
		var c model.InterestCategory
		if err := rows.Scan(&c.ID, &c.UserID, &c.Category, &c.Weight, &c.LastReinforcedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func loadInterestTopics(ctx context.Context, tx *sql.Tx) ([]model.InterestTopic, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, topic, weight, last_reinforced_at
		FROM interest_topics ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.InterestTopic
	for rows.Next() {
		var t model.InterestTopic
		if err := rows.Scan(&t.ID, &t.Topic, &t.Weight, &t.LastReinforcedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadUserTags(ctx context.Context, tx *sql.Tx) ([]model.UserTag, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, user_id, name, created_at FROM user_tags ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadArticleUserTags(ctx context.Context, tx *sql.Tx) ([]ArticleUserTagRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT article_id, tag_id, user_id, created_at
		FROM article_user_tags ORDER BY article_id, tag_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArticleUserTagRow
	for rows.Next() {
		var r ArticleUserTagRow
		if err := rows.Scan(&r.ArticleID, &r.TagID, &r.UserID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadUserPreferences(ctx context.Context, tx *sql.Tx) ([]model.UserPreference, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(user_id, 0), article_id, signal_type, signal_value, created_at
		FROM user_preferences ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.UserPreference
	for rows.Next() {
		var p model.UserPreference
		if err := rows.Scan(&p.ID, &p.UserID, &p.ArticleID, &p.SignalType, &p.SignalValue, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WriteFile serializes the snapshot to <dir>/<filename>.json. Filename is
// derived from CreatedAt. Returns the absolute path.
func WriteFile(s *Snapshot, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	name := fileNamePrefix + s.CreatedAt.UTC().Format(fileTimeLayout) + fileNameSuffix
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	// Write atomically: tmp + rename so a partial write never leaves a
	// truncated file that List would treat as valid.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// WriteFiles writes the metadata snapshot (①) and the saved-archive sibling
// (②) for one timestamp. Order is sibling-first so that ① — the file
// List() enumerates — is the commit pointer: if we crash between writes,
// an orphan ② is invisible to List and gets swept by the next Prune.
//
// Returns the absolute metadata path and the saved sibling path.
func WriteFiles(s *Snapshot, ss *SavedSnapshot, dir string) (metadataPath, savedPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	name := fileNamePrefix + s.CreatedAt.UTC().Format(fileTimeLayout) + fileNameSuffix
	metadataPath = filepath.Join(dir, name)
	savedPath = savedSiblingPath(metadataPath)

	if err := WriteSavedFile(ss, metadataPath); err != nil {
		return "", "", fmt.Errorf("write saved sibling: %w", err)
	}

	if _, err := WriteFile(s, dir); err != nil {
		os.Remove(savedPath)
		return "", "", fmt.Errorf("write metadata: %w", err)
	}
	return metadataPath, savedPath, nil
}

// Load reads and parses a snapshot file from disk.
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return &s, nil
}

// List returns all backup files in dir, newest first. Files whose names don't
// match the expected layout are silently skipped — we only own files we wrote.
func List(dir string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t, ok := parseFilename(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileInfo{Name: e.Name(), CreatedAt: t, Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// parseFilename extracts the timestamp embedded in a backup filename, or
// reports !ok if the name isn't one of ours.
func parseFilename(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, fileNamePrefix) || !strings.HasSuffix(name, fileNameSuffix) {
		return time.Time{}, false
	}
	core := strings.TrimSuffix(strings.TrimPrefix(name, fileNamePrefix), fileNameSuffix)
	t, err := time.ParseInLocation(fileTimeLayout, core, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
