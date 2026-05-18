// Package backup snapshots subscription-side state to JSON files on disk and
// applies a tiered retention policy. Articles are intentionally excluded —
// they are re-fetched by the worker from the feed URLs.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
//
// Two canonical on-disk shapes exist:
//
//	rss-pal-backup-<ts>.tar.gz   — single bundle (current format)
//	rss-pal-backup-<ts>.json     — legacy metadata, optionally paired with
//	                               .saved.json.gz sibling
//
// Both are listable / restorable. New backups are always written as .tar.gz;
// legacy pairs survive until migrated by cmd/backup_migrate.
const (
	fileNamePrefix    = "rss-pal-backup-"
	fileNameSuffix    = ".json"
	tarballFileSuffix = ".tar.gz"
	fileTimeLayout    = "20060102-150405"
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
//
// HasSaved tells the UI whether saved-article data is included:
//   - .tar.gz : true if the archive contains a *.saved.json.gz member
//   - .json   : true if a paired .saved.json.gz sibling sits next to it
type FileInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	HasSaved  bool      `json:"has_saved"`
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
		SELECT id, user_id, topic, weight, last_reinforced_at
		FROM interest_topics ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.InterestTopic
	for rows.Next() {
		var t model.InterestTopic
		var userID sql.NullInt64
		if err := rows.Scan(&t.ID, &userID, &t.Topic, &t.Weight, &t.LastReinforcedAt); err != nil {
			return nil, err
		}
		if userID.Valid {
			uid := int(userID.Int64)
			t.UserID = &uid
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

// WriteFiles writes a single self-contained .tar.gz that bundles the
// metadata snapshot and the saved-archive sibling for one timestamp. Atomic
// via .tmp + rename — a crash mid-write leaves no half-written file visible
// to List. Returns the absolute archive path. The savedPath return value is
// the inner member name (preserved for caller back-compat with the prior
// pair-writing signature) and does not exist on disk as a separate file.
func WriteFiles(s *Snapshot, ss *SavedSnapshot, dir string) (archivePath, savedMemberName string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	stamp := s.CreatedAt.UTC().Format(fileTimeLayout)
	archivePath = filepath.Join(dir, fileNamePrefix+stamp+tarballFileSuffix)
	metaMemberName := fileNamePrefix + stamp + fileNameSuffix
	savedMemberName = fileNamePrefix + stamp + savedFileSuffix

	metaBytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	savedBytes, err := encodeSavedGzip(ss)
	if err != nil {
		return "", "", fmt.Errorf("encode saved member: %w", err)
	}

	tmp := archivePath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", tmp, err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	if err := writeTarMember(tw, metaMemberName, metaBytes, s.CreatedAt); err != nil {
		tw.Close()
		gz.Close()
		f.Close()
		os.Remove(tmp)
		return "", "", err
	}
	if err := writeTarMember(tw, savedMemberName, savedBytes, s.CreatedAt); err != nil {
		tw.Close()
		gz.Close()
		f.Close()
		os.Remove(tmp)
		return "", "", err
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		f.Close()
		os.Remove(tmp)
		return "", "", fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", "", fmt.Errorf("close gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", "", fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, archivePath); err != nil {
		os.Remove(tmp)
		return "", "", fmt.Errorf("rename %s -> %s: %w", tmp, archivePath, err)
	}
	return archivePath, savedMemberName, nil
}

func writeTarMember(tw *tar.Writer, name string, data []byte, modTime time.Time) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: modTime,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("tar body %s: %w", name, err)
	}
	return nil
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
// match the expected layout (.tar.gz or legacy .json) are silently skipped —
// we only own files we wrote. When two entries share a timestamp the .tar.gz
// wins, so a half-completed migration (legacy pair + new tarball) lists once.
func List(dir string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	siblings := make(map[string]struct{})
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), savedFileSuffix) {
			siblings[e.Name()] = struct{}{}
		}
	}

	byStamp := make(map[time.Time]FileInfo)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t, isTar, ok := parseFilenameAny(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fi := FileInfo{Name: e.Name(), CreatedAt: t, Size: info.Size()}
		if isTar {
			fi.HasSaved = tarballHasSaved(filepath.Join(dir, e.Name()))
		} else {
			_, fi.HasSaved = siblings[savedSiblingName(e.Name())]
		}
		// .tar.gz wins on collision with the same timestamp.
		if existing, dup := byStamp[t]; dup && !isTar && strings.HasSuffix(existing.Name, tarballFileSuffix) {
			continue
		}
		byStamp[t] = fi
	}

	out := make([]FileInfo, 0, len(byStamp))
	for _, fi := range byStamp {
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// parseFilename extracts the timestamp embedded in a legacy .json backup
// filename. Kept for the orphan-sibling sweep and existing tests; new code
// should use parseFilenameAny which also accepts .tar.gz.
func parseFilename(name string) (time.Time, bool) {
	t, _, ok := parseFilenameAny(name)
	return t, ok
}

// parseFilenameAny accepts either the .tar.gz canonical format or the legacy
// .json metadata. Returns the parsed timestamp and a flag for which suffix
// matched.
func parseFilenameAny(name string) (ts time.Time, isTarball bool, ok bool) {
	if !strings.HasPrefix(name, fileNamePrefix) {
		return time.Time{}, false, false
	}
	core := strings.TrimPrefix(name, fileNamePrefix)
	switch {
	case strings.HasSuffix(core, tarballFileSuffix):
		core = strings.TrimSuffix(core, tarballFileSuffix)
		isTarball = true
	case strings.HasSuffix(core, fileNameSuffix):
		core = strings.TrimSuffix(core, fileNameSuffix)
	default:
		return time.Time{}, false, false
	}
	t, err := time.ParseInLocation(fileTimeLayout, core, time.UTC)
	if err != nil {
		return time.Time{}, false, false
	}
	return t, isTarball, true
}

// tarballHasSaved peeks the tarball for a .saved.json.gz member. Returns
// false on any I/O or format error — List doesn't surface those, the listing
// just shows HasSaved=false and the user finds out at restore time.
func tarballHasSaved(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return false
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return false
		}
		if strings.HasSuffix(strings.ToLower(hdr.Name), savedFileSuffix) {
			return true
		}
	}
}

// ExtractTarball opens a backup .tar.gz and writes the .json metadata member
// to metaDst plus, if present, the .saved.json.gz member to savedDst. Returns
// (hasSaved, error). Members with paths containing `..` or starting with `/`
// are rejected (tar-slip), and the archive must contain a .json member
// otherwise it's malformed and an error is returned — callers should NOT
// silently fall back to a legacy-restore path on this error.
func ExtractTarball(path, metaDst, savedDst string) (hasSaved bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return extractTarballStream(f, metaDst, savedDst)
}

// ExtractTarballStream is the streaming variant of ExtractTarball — useful
// when the source is an uploaded multipart file that hasn't been written to
// disk yet. Same validation rules.
func ExtractTarballStream(src io.Reader, metaDst, savedDst string) (hasSaved bool, err error) {
	return extractTarballStream(src, metaDst, savedDst)
}

func extractTarballStream(src io.Reader, metaDst, savedDst string) (hasSaved bool, err error) {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return false, fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var foundMeta bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == "" || base == "." || strings.Contains(hdr.Name, "..") || strings.HasPrefix(hdr.Name, "/") {
			return false, fmt.Errorf("rejected tar entry: %q", hdr.Name)
		}
		lower := strings.ToLower(base)
		switch {
		case strings.HasSuffix(lower, savedFileSuffix):
			if err := copyToFile(tr, savedDst); err != nil {
				return false, fmt.Errorf("write saved member: %w", err)
			}
			hasSaved = true
		case strings.HasSuffix(lower, fileNameSuffix):
			if err := copyToFile(tr, metaDst); err != nil {
				return false, fmt.Errorf("write metadata member: %w", err)
			}
			foundMeta = true
		}
	}
	if !foundMeta {
		return false, fmt.Errorf("archive contains no .json metadata member")
	}
	return hasSaved, nil
}

func copyToFile(src io.Reader, dst string) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
