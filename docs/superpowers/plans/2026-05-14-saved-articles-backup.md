# 网摘 Backup & Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing backup module to snapshot and restore the user's saved articles (网摘), so a wipe + restore preserves bookmarklet-captured content and save-signaled RSS articles, plus their tags / signals / reading_progress.

**Architecture:** Each backup timestamp now produces a pair of files — the unchanged metadata `.json` (the "commit pointer") plus a new gzipped `.saved.json.gz` sibling for saved-article bodies. Restore loads both, upserts saved articles to build an `old_article_id → new_article_id` map, then uses that map to also restore the `article_user_tags` and `user_preferences` rows that today are skipped under `SkippedArticleLink`. Pairing is by filename, write order is sibling-first/metadata-last so a partial write never produces a visible-but-incomplete backup.

**Tech Stack:** Go 1.x, `database/sql` + `lib/pq` (existing pattern — no ORM), `encoding/json`, `compress/gzip`. Tests are pure unit tests (matching the existing snapshot/retention test style — no DB fixtures).

**Spec:** `docs/superpowers/specs/2026-05-14-saved-articles-backup-design.md`

---

## File Structure

**New:**
- `backend/internal/backup/snapshot_saved.go` — `SavedSnapshot`, `SavedArticleRow`, `ReadingProgressRow` types; sibling-path helper; `BuildSaved`; `WriteSavedFile`; `LoadSaved`.
- `backend/internal/backup/snapshot_saved_test.go` — Round-trip, sibling-path, missing-sibling tests.

**Modified:**
- `backend/internal/backup/snapshot.go` — `Build` returns both snapshots; new `WriteFiles` writes the pair atomically (② first, ① last).
- `backend/internal/backup/restore.go` — `Restore` takes both snapshots; new fields on `RestoreStats`; article-id mapping flow.
- `backend/internal/backup/retention.go` — `Prune` deletes sibling and sweeps orphan `.saved.json.gz` files.
- `backend/internal/backup/retention_test.go` — Sibling deletion and orphan sweep cases.
- `backend/internal/backup/trigger.go` — `RunNow` uses new `Build` + `WriteFiles`.
- `backend/internal/api/admin.go` — `RestoreBackup` also loads the saved sibling and passes it.
- `backend/internal/api/bookmarklet.go` — `WithBackupRunner` setter; `Capture` triggers debounced backup on success.
- `backend/cmd/server/main.go` — Wire `bookmarkletHandler.WithBackupRunner(backupRunner)`.

---

## Task 1: SavedSnapshot types and sibling-path helper

**Files:**
- Create: `backend/internal/backup/snapshot_saved.go`
- Create: `backend/internal/backup/snapshot_saved_test.go`

### Step 1.1: Write the failing test for sibling-path derivation

- [ ] Create `backend/internal/backup/snapshot_saved_test.go`:

```go
package backup

import "testing"

func TestSavedSiblingPath(t *testing.T) {
	cases := []struct {
		metadata, want string
	}{
		{
			metadata: "/tmp/backups/rss-pal-backup-20260514-093015.json",
			want:     "/tmp/backups/rss-pal-backup-20260514-093015.saved.json.gz",
		},
		{
			metadata: "rss-pal-backup-20260101-000000.json",
			want:     "rss-pal-backup-20260101-000000.saved.json.gz",
		},
		{
			metadata: "/tmp/x/y/foo.json",
			want:     "/tmp/x/y/foo.saved.json.gz",
		},
	}
	for _, c := range cases {
		got := savedSiblingPath(c.metadata)
		if got != c.want {
			t.Errorf("savedSiblingPath(%q) = %q, want %q", c.metadata, got, c.want)
		}
	}
}
```

### Step 1.2: Run the test — expect compile failure

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run TestSavedSiblingPath
```
Expected: `undefined: savedSiblingPath`

### Step 1.3: Create snapshot_saved.go with types and helper

- [ ] Create `backend/internal/backup/snapshot_saved.go`:

```go
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
```

### Step 1.4: Run the test — expect pass

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run TestSavedSiblingPath -v
```
Expected: `PASS`

### Step 1.5: Commit

- [ ] Run:
```bash
git add backend/internal/backup/snapshot_saved.go backend/internal/backup/snapshot_saved_test.go
git commit -m "feat(backup): add SavedSnapshot types and sibling-path helper"
```

---

## Task 2: Gzip write/read for SavedSnapshot

**Files:**
- Modify: `backend/internal/backup/snapshot_saved.go` (add `WriteSavedFile`, `LoadSaved`)
- Modify: `backend/internal/backup/snapshot_saved_test.go` (round-trip + missing-sibling tests)

### Step 2.1: Write the failing tests

- [ ] Append to `backend/internal/backup/snapshot_saved_test.go`:

```go
import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestWriteAndLoadSavedFile(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "rss-pal-backup-20260514-093015.json")

	pub := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	ss := &SavedSnapshot{
		Version:   SavedSnapshotVersion,
		CreatedAt: time.Date(2026, 5, 14, 9, 30, 15, 0, time.UTC),
		SavedArticles: []SavedArticleRow{{
			ExportID:    42,
			FeedURL:     "bookmarklet://user/1",
			Title:       "测试标题",
			URL:         "https://example.com/post",
			Content:     "<p>hello 网摘</p>",
			PublishedAt: &pub,
			FetchedAt:   time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC),
			WordCount:   3,
			IsRead:      true,
		}},
		ReadingProgress: []ReadingProgressRow{{
			UserID:          1,
			ArticleExportID: 42,
			ScrollPosition:  0.5,
			LastReadAt:      time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC),
			IsCompleted:     false,
		}},
	}

	if err := WriteSavedFile(ss, metaPath); err != nil {
		t.Fatalf("WriteSavedFile: %v", err)
	}

	// File should exist at sibling path and have .gz suffix.
	savedPath := savedSiblingPath(metaPath)
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("expected saved file at %s: %v", savedPath, err)
	}

	loaded, err := LoadSaved(metaPath)
	if err != nil {
		t.Fatalf("LoadSaved: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSaved returned nil for existing sibling")
	}
	if !reflect.DeepEqual(loaded.SavedArticles, ss.SavedArticles) {
		t.Errorf("SavedArticles roundtrip mismatch:\n got %+v\nwant %+v", loaded.SavedArticles, ss.SavedArticles)
	}
	if !reflect.DeepEqual(loaded.ReadingProgress, ss.ReadingProgress) {
		t.Errorf("ReadingProgress roundtrip mismatch")
	}
}

func TestLoadSavedMissingSibling(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "rss-pal-backup-20260514-093015.json")
	// No sibling written.
	got, err := LoadSaved(metaPath)
	if err != nil {
		t.Fatalf("LoadSaved on missing sibling: unexpected err %v", err)
	}
	if got != nil {
		t.Errorf("LoadSaved on missing sibling: want nil, got %+v", got)
	}
}

func TestWriteSavedFileAtomic(t *testing.T) {
	// After WriteSavedFile returns, no .tmp leftover should remain.
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "rss-pal-backup-20260514-093015.json")
	ss := &SavedSnapshot{Version: 1, CreatedAt: time.Now().UTC()}
	if err := WriteSavedFile(ss, metaPath); err != nil {
		t.Fatalf("WriteSavedFile: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}
```

### Step 2.2: Run tests — expect compile failure

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run "TestWriteAndLoadSavedFile|TestLoadSavedMissingSibling|TestWriteSavedFileAtomic"
```
Expected: `undefined: WriteSavedFile` / `undefined: LoadSaved`

### Step 2.3: Implement WriteSavedFile and LoadSaved

- [ ] Append to `backend/internal/backup/snapshot_saved.go`:

```go
import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
)

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
		return nil, err
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
```

### Step 2.4: Run the tests — expect pass

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run "TestWriteAndLoadSavedFile|TestLoadSavedMissingSibling|TestWriteSavedFileAtomic" -v
```
Expected: `PASS` for all three.

### Step 2.5: Commit

- [ ] Run:
```bash
git add backend/internal/backup/snapshot_saved.go backend/internal/backup/snapshot_saved_test.go
git commit -m "feat(backup): gzip write/load for SavedSnapshot sibling files"
```

---

## Task 3: BuildSaved query + Build signature change

**Files:**
- Modify: `backend/internal/backup/snapshot_saved.go` (add `buildSaved` query)
- Modify: `backend/internal/backup/snapshot.go` (change `Build` signature)
- Modify: `backend/internal/backup/trigger.go` (update `RunNow` caller)

### Step 3.1: Add buildSaved to snapshot_saved.go

The query joins `articles → feeds`, matches the saved predicate, excludes link_set children. Reading_progress is filtered by the saved article IDs in a second query.

- [ ] Append to `backend/internal/backup/snapshot_saved.go`:

```go
import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// buildSaved runs inside the same read-only transaction as Build. It returns
// the SavedSnapshot for the current DB state; never returns nil for an empty
// DB (returns an empty-slice snapshot instead so the file always exists in a
// recognizable form).
func buildSaved(ctx context.Context, tx *sql.Tx, createdAt time.Time) (*SavedSnapshot, error) {
	ss := &SavedSnapshot{
		Version:   SavedSnapshotVersion,
		CreatedAt: createdAt,
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, f.url, COALESCE(a.title, ''), a.url, COALESCE(a.content, ''),
		       a.published_at,
		       COALESCE(a.summary_brief, ''), COALESCE(a.summary_detailed, ''),
		       a.fetched_at, a.word_count, a.reading_minutes, a.is_read,
		       COALESCE(a.editor_note, ''),
		       COALESCE(a.media_url, ''), COALESCE(a.media_type, ''), COALESCE(a.media_duration_seconds, 0)
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.parent_article_id IS NULL
		  AND (
		    EXISTS (
		      SELECT 1 FROM user_preferences p
		      WHERE p.article_id = a.id AND p.signal_type = 'save'
		    )
		    OR f.feed_type = 'saved'
		  )
		ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var savedIDs []int
	for rows.Next() {
		var r SavedArticleRow
		if err := rows.Scan(&r.ExportID, &r.FeedURL, &r.Title, &r.URL, &r.Content,
			&r.PublishedAt,
			&r.SummaryBrief, &r.SummaryDetailed,
			&r.FetchedAt, &r.WordCount, &r.ReadingMinutes, &r.IsRead,
			&r.EditorNote,
			&r.MediaURL, &r.MediaType, &r.MediaDurationSeconds); err != nil {
			return nil, err
		}
		ss.SavedArticles = append(ss.SavedArticles, r)
		savedIDs = append(savedIDs, r.ExportID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(savedIDs) == 0 {
		return ss, nil
	}

	progRows, err := tx.QueryContext(ctx, `
		SELECT user_id, article_id, scroll_position, last_read_at, is_completed
		FROM reading_progress
		WHERE article_id = ANY($1)
		ORDER BY user_id, article_id`, pq.Array(savedIDs))
	if err != nil {
		return nil, err
	}
	defer progRows.Close()

	for progRows.Next() {
		var p ReadingProgressRow
		if err := progRows.Scan(&p.UserID, &p.ArticleExportID, &p.ScrollPosition, &p.LastReadAt, &p.IsCompleted); err != nil {
			return nil, err
		}
		ss.ReadingProgress = append(ss.ReadingProgress, p)
	}
	return ss, progRows.Err()
}
```

### Step 3.2: Change Build signature in snapshot.go

- [ ] Edit `backend/internal/backup/snapshot.go`: change the `Build` function. Replace:

```go
func Build(ctx context.Context, db *sql.DB) (*Snapshot, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	s := &Snapshot{
		Version:   SnapshotVersion,
		CreatedAt: time.Now().UTC(),
	}

	if s.Feeds, err = loadFeeds(ctx, tx); err != nil {
		return nil, fmt.Errorf("load feeds: %w", err)
	}
	if s.InterestCategories, err = loadInterestCategories(ctx, tx); err != nil {
		return nil, fmt.Errorf("load interest_categories: %w", err)
	}
	if s.InterestTopics, err = loadInterestTopics(ctx, tx); err != nil {
		return nil, fmt.Errorf("load interest_topics: %w", err)
	}
	if s.UserTags, err = loadUserTags(ctx, tx); err != nil {
		return nil, fmt.Errorf("load user_tags: %w", err)
	}
	if s.ArticleUserTags, err = loadArticleUserTags(ctx, tx); err != nil {
		return nil, fmt.Errorf("load article_user_tags: %w", err)
	}
	if s.UserPreferences, err = loadUserPreferences(ctx, tx); err != nil {
		return nil, fmt.Errorf("load user_preferences: %w", err)
	}

	return s, nil
}
```

…with:

```go
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
```

### Step 3.3: Update RunNow in trigger.go

- [ ] Edit `backend/internal/backup/trigger.go`. Replace the body of `RunNow`:

```go
func (r *Runner) RunNow(ctx context.Context) error {
	r.inflight.Lock()
	defer r.inflight.Unlock()

	s, err := Build(ctx, r.db)
	if err != nil {
		return err
	}
	path, err := WriteFile(s, r.dir)
	if err != nil {
		return err
	}
	removed, err := Prune(r.dir, time.Now(), DefaultRetention)
	if err != nil {
		log.Printf("backup: wrote %s but prune failed: %v", path, err)
		return nil
	}
	if len(removed) > 0 {
		log.Printf("backup: wrote %s, pruned %d old files", path, len(removed))
	} else {
		log.Printf("backup: wrote %s", path)
	}
	return nil
}
```

…with:

```go
func (r *Runner) RunNow(ctx context.Context) error {
	r.inflight.Lock()
	defer r.inflight.Unlock()

	s, ss, err := Build(ctx, r.db)
	if err != nil {
		return err
	}
	metaPath, _, err := WriteFiles(s, ss, r.dir)
	if err != nil {
		return err
	}
	removed, err := Prune(r.dir, time.Now(), DefaultRetention)
	if err != nil {
		log.Printf("backup: wrote %s but prune failed: %v", metaPath, err)
		return nil
	}
	if len(removed) > 0 {
		log.Printf("backup: wrote %s (+saved sibling), pruned %d old files", metaPath, len(removed))
	} else {
		log.Printf("backup: wrote %s (+saved sibling)", metaPath)
	}
	return nil
}
```

### Step 3.4: Verify it compiles (WriteFiles doesn't exist yet — compile fails)

- [ ] Run:
```bash
cd backend && go build ./internal/backup/...
```
Expected: `undefined: WriteFiles` — this is expected; WriteFiles is added in Task 4.

Do NOT commit yet — finish Task 4 first so the tree compiles.

---

## Task 4: WriteFiles writes the pair atomically

**Files:**
- Modify: `backend/internal/backup/snapshot.go` (add `WriteFiles`)
- Modify: `backend/internal/backup/snapshot_saved_test.go` (pair write test)

### Step 4.1: Write the failing test

- [ ] Append to `backend/internal/backup/snapshot_saved_test.go`:

```go
import (
	"github.com/bytedance/rss-pal/internal/model"
)

func TestWriteFilesWritesPair(t *testing.T) {
	dir := t.TempDir()
	owner := 7
	created := time.Date(2026, 5, 14, 9, 30, 15, 0, time.UTC)

	s := &Snapshot{
		Version:   SnapshotVersion,
		CreatedAt: created,
		Feeds: []model.Feed{
			{ID: 1, URL: "bookmarklet://user/7", Title: "⭐ 网摘", OwnerID: &owner, FeedType: "saved", Status: "active", IsActive: true},
		},
	}
	ss := &SavedSnapshot{
		Version:   SavedSnapshotVersion,
		CreatedAt: created,
		SavedArticles: []SavedArticleRow{{
			ExportID: 1,
			FeedURL:  "bookmarklet://user/7",
			Title:    "x",
			URL:      "https://example.com/a",
			Content:  "body",
			FetchedAt: created,
		}},
	}

	metaPath, savedPath, err := WriteFiles(s, ss, dir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}

	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("metadata file missing: %v", err)
	}
	if _, err := os.Stat(savedPath); err != nil {
		t.Errorf("saved file missing: %v", err)
	}

	// Round-trip both halves through their public Load functions.
	loadedS, err := Load(metaPath)
	if err != nil {
		t.Fatalf("Load metadata: %v", err)
	}
	if len(loadedS.Feeds) != 1 || loadedS.Feeds[0].URL != "bookmarklet://user/7" {
		t.Errorf("metadata feeds roundtrip mismatch: %+v", loadedS.Feeds)
	}

	loadedSS, err := LoadSaved(metaPath)
	if err != nil {
		t.Fatalf("LoadSaved: %v", err)
	}
	if loadedSS == nil || len(loadedSS.SavedArticles) != 1 || loadedSS.SavedArticles[0].URL != "https://example.com/a" {
		t.Errorf("saved roundtrip mismatch: %+v", loadedSS)
	}
}
```

### Step 4.2: Run the test — expect compile failure

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run TestWriteFilesWritesPair
```
Expected: `undefined: WriteFiles`

### Step 4.3: Implement WriteFiles in snapshot.go

- [ ] Append to `backend/internal/backup/snapshot.go` (after `WriteFile`):

```go
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
	// Compute the metadata filename early so we can derive the sibling path
	// before either file is written.
	name := fileNamePrefix + s.CreatedAt.UTC().Format(fileTimeLayout) + fileNameSuffix
	metadataPath = filepath.Join(dir, name)
	savedPath = savedSiblingPath(metadataPath)

	// Step 1: write sibling ② first.
	if err := WriteSavedFile(ss, metadataPath); err != nil {
		return "", "", fmt.Errorf("write saved sibling: %w", err)
	}

	// Step 2: write metadata ① — the commit pointer.
	if _, err := WriteFile(s, dir); err != nil {
		// Best-effort cleanup of the orphan sibling so the dir doesn't grow
		// junk on every failure. If unlink fails we let Prune sweep it later.
		os.Remove(savedPath)
		return "", "", fmt.Errorf("write metadata: %w", err)
	}
	return metadataPath, savedPath, nil
}
```

### Step 4.4: Run the test — expect pass

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run TestWriteFilesWritesPair -v
```
Expected: `PASS`

### Step 4.5: Run all backup tests — verify Task 3's changes didn't break existing tests

- [ ] Run:
```bash
cd backend && go test ./internal/backup/... -v
```
Expected: all PASS (existing `TestWriteFileAndLoad` and retention tests still pass).

### Step 4.6: Commit

- [ ] Run:
```bash
git add backend/internal/backup/snapshot.go backend/internal/backup/snapshot_saved.go backend/internal/backup/snapshot_saved_test.go backend/internal/backup/trigger.go
git commit -m "feat(backup): Build returns both snapshots; WriteFiles writes pair"
```

---

## Task 5: Restore with article-id mapping

**Files:**
- Modify: `backend/internal/backup/restore.go` (signature + new flow)
- Modify: `backend/internal/api/admin.go` (load saved sibling, pass through)

### Step 5.1: Replace restore.go

- [ ] Replace the entire contents of `backend/internal/backup/restore.go`:

```go
package backup

import (
	"context"
	"database/sql"
	"fmt"
)

// RestoreStats summarizes what changed during a Restore call.
//
// SavedArticles, ReadingProgress, ArticleUserTags, UserPreferences count rows
// the new flow inserts (or skips because the unique key already existed —
// existing rows are intentionally preserved).
//
// SkippedMissingFeed is incremented when a SavedArticleRow's FeedURL doesn't
// resolve to any feed in the restored DB (should not happen if the backup is
// internally consistent).
//
// SkippedArticleLink counts ArticleUserTag / UserPreference rows whose
// article_id is NOT in the saved set — these reference non-backed-up RSS
// articles and remain unrestorable.
type RestoreStats struct {
	Feeds              int `json:"feeds"`
	UserTags           int `json:"user_tags"`
	InterestCategories int `json:"interest_categories"`
	InterestTopics     int `json:"interest_topics"`
	SavedArticles      int `json:"saved_articles"`
	ReadingProgress    int `json:"reading_progress"`
	ArticleUserTags    int `json:"article_user_tags"`
	UserPreferences    int `json:"user_preferences"`
	SkippedArticleLink int `json:"skipped_article_link"`
	SkippedMissingFeed int `json:"skipped_missing_feed"`
}

// Restore applies a backup pair (metadata + saved) to the database, in one
// transaction. ss may be nil (legacy backup with no sibling); in that case
// behavior matches the pre-saved-snapshot version: saved articles aren't
// restored, and every ArticleUserTag / UserPreference goes to
// SkippedArticleLink.
//
// Restore is additive — existing rows are preserved on conflict. The freshest
// local state wins for content/signal columns; the backup only fills gaps.
func Restore(ctx context.Context, db *sql.DB, s *Snapshot, ss *SavedSnapshot) (RestoreStats, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreStats{}, err
	}
	defer tx.Rollback()

	var stats RestoreStats

	// 1. Feeds (upsert by URL; backup wins on updatable cols).
	for i := range s.Feeds {
		f := &s.Feeds[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO feeds (url, title, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (url) DO UPDATE SET
				title = EXCLUDED.title,
				fetch_interval_minutes = EXCLUDED.fetch_interval_minutes,
				is_active = EXCLUDED.is_active,
				owner_id = EXCLUDED.owner_id,
				feed_type = EXCLUDED.feed_type,
				status = EXCLUDED.status,
				priority_weight = EXCLUDED.priority_weight`,
			f.URL, f.Title, f.FetchIntervalMin, f.ETag, f.LastModified, f.IsActive,
			ownerOrNil(f.OwnerID), f.FeedType, f.Status, f.PriorityWeight, f.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert feed %s: %w", f.URL, err)
		}
		stats.Feeds++
	}

	// 2. User tags (DB wins — never overwrite existing tag names).
	for i := range s.UserTags {
		t := &s.UserTags[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_tags (user_id, name, created_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, name) DO NOTHING`,
			t.UserID, t.Name, t.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert user_tag (%d,%s): %w", t.UserID, t.Name, err)
		}
		stats.UserTags++
	}

	// 3. Interest categories (backup wins on weight).
	for i := range s.InterestCategories {
		c := &s.InterestCategories[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO interest_categories (user_id, category, weight, last_reinforced_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, category) DO UPDATE SET
				weight = EXCLUDED.weight,
				last_reinforced_at = EXCLUDED.last_reinforced_at`,
			c.UserID, c.Category, c.Weight, c.LastReinforcedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert interest_category (%d,%s): %w", c.UserID, c.Category, err)
		}
		stats.InterestCategories++
	}

	// 4. Interest topics (backup wins).
	for i := range s.InterestTopics {
		t := &s.InterestTopics[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO interest_topics (topic, weight, last_reinforced_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (topic) DO UPDATE SET
				weight = EXCLUDED.weight,
				last_reinforced_at = EXCLUDED.last_reinforced_at`,
			t.Topic, t.Weight, t.LastReinforcedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert interest_topic %s: %w", t.Topic, err)
		}
		stats.InterestTopics++
	}

	// 5. Saved articles: insert with DO NOTHING on conflict. Build the
	//    old→new id map regardless of whether the row was inserted or already
	//    existed — the map drives the next three steps.
	idMap := make(map[int]int)
	if ss != nil {
		feedIDByURL, err := loadFeedIDByURL(ctx, tx)
		if err != nil {
			return stats, fmt.Errorf("load feed id map: %w", err)
		}
		for i := range ss.SavedArticles {
			ar := &ss.SavedArticles[i]
			feedID, ok := feedIDByURL[ar.FeedURL]
			if !ok {
				stats.SkippedMissingFeed++
				continue
			}
			var newID int
			err := tx.QueryRowContext(ctx, `
				INSERT INTO articles (
					feed_id, title, url, content, published_at,
					summary_brief, summary_detailed, fetched_at,
					word_count, reading_minutes, is_read, editor_note,
					media_url, media_type, media_duration_seconds
				) VALUES ($1,$2,$3,$4,$5, $6,$7,$8, $9,$10,$11,$12, $13,$14,$15)
				ON CONFLICT (feed_id, url) WHERE parent_article_id IS NULL
				DO NOTHING
				RETURNING id`,
				feedID, ar.Title, ar.URL, ar.Content, ar.PublishedAt,
				ar.SummaryBrief, ar.SummaryDetailed, ar.FetchedAt,
				ar.WordCount, ar.ReadingMinutes, ar.IsRead, ar.EditorNote,
				ar.MediaURL, ar.MediaType, ar.MediaDurationSeconds,
			).Scan(&newID)
			if err == sql.ErrNoRows {
				// Conflict — row exists. Look it up to populate idMap.
				err = tx.QueryRowContext(ctx, `
					SELECT id FROM articles
					WHERE feed_id = $1 AND url = $2 AND parent_article_id IS NULL`,
					feedID, ar.URL).Scan(&newID)
			}
			if err != nil {
				return stats, fmt.Errorf("upsert saved article %s: %w", ar.URL, err)
			}
			idMap[ar.ExportID] = newID
			stats.SavedArticles++
		}

		// 6. Reading progress for saved articles (DB wins on conflict).
		for i := range ss.ReadingProgress {
			rp := &ss.ReadingProgress[i]
			newAID, ok := idMap[rp.ArticleExportID]
			if !ok {
				continue
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (article_id) DO NOTHING`,
				rp.UserID, newAID, rp.ScrollPosition, rp.LastReadAt, rp.IsCompleted)
			if err != nil {
				return stats, fmt.Errorf("upsert reading_progress (%d,%d): %w", rp.UserID, newAID, err)
			}
			stats.ReadingProgress++
		}
	}

	// 7. Article-user-tag join: translate via idMap for saved articles;
	//    everything else is unrestorable.
	for i := range s.ArticleUserTags {
		row := &s.ArticleUserTags[i]
		newAID, ok := idMap[row.ArticleID]
		if !ok {
			stats.SkippedArticleLink++
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO article_user_tags (article_id, tag_id, user_id, created_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (article_id, tag_id) DO NOTHING`,
			newAID, row.TagID, row.UserID, row.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert article_user_tag (%d,%d): %w", newAID, row.TagID, err)
		}
		stats.ArticleUserTags++
	}

	// 8. User preferences (save / like / dislike signals) for saved articles.
	for i := range s.UserPreferences {
		p := &s.UserPreferences[i]
		newAID, ok := idMap[p.ArticleID]
		if !ok {
			stats.SkippedArticleLink++
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_preferences (user_id, article_id, signal_type, signal_value, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, article_id, signal_type) DO NOTHING`,
			p.UserID, newAID, p.SignalType, p.SignalValue, p.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert user_preference (%d,%d,%s): %w", p.UserID, newAID, p.SignalType, err)
		}
		stats.UserPreferences++
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func loadFeedIDByURL(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, url FROM feeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var id int
		var u string
		if err := rows.Scan(&id, &u); err != nil {
			return nil, err
		}
		m[u] = id
	}
	return m, rows.Err()
}

func ownerOrNil(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
```

### Step 5.2: Verify ON CONFLICT clauses match existing unique constraints

- [ ] Check that the three conflict targets used above exist as unique constraints/indexes in current schema:

```bash
grep -rn "UNIQUE\|uniq_" backend/migrations/ | grep -E "user_preferences|reading_progress|article_user_tags"
```
Expected: see unique constraints on `user_preferences(user_id, article_id, signal_type)`, `reading_progress(article_id)`, `article_user_tags(article_id, tag_id)`. If any of these don't exist with that exact column list, fix the conflict target in `restore.go` to match the real constraint name before continuing.

If the unique on `user_preferences` does not include `signal_type` (just `(user_id, article_id)`), change the SQL to `ON CONFLICT (user_id, article_id) DO NOTHING` — that's safe because the backup is restoring the latest snapshot of signals.

### Step 5.3: Update admin.go to pass the saved sibling

- [ ] In `backend/internal/api/admin.go`, replace the body of `RestoreBackup` after the path-resolution block:

Find:
```go
	path := filepath.Join(h.cfg.Backup.Dir, req.Name)
	s, err := backup.Load(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	stats, err := backup.Restore(c.Request.Context(), h.db, s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "stats": stats})
```

Replace with:
```go
	path := filepath.Join(h.cfg.Backup.Dir, req.Name)
	s, err := backup.Load(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	// Sibling may be absent (legacy backup pre-saved-snapshot) — LoadSaved
	// returns (nil, nil) in that case and Restore handles it.
	ss, err := backup.LoadSaved(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load saved sibling: " + err.Error()})
		return
	}
	stats, err := backup.Restore(c.Request.Context(), h.db, s, ss)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "stats": stats})
```

### Step 5.4: Verify build

- [ ] Run:
```bash
cd backend && go build ./...
```
Expected: clean build.

### Step 5.5: Run all tests

- [ ] Run:
```bash
cd backend && go test ./internal/backup/... -v
```
Expected: existing tests still PASS (restore has no DB-driven test yet, matching existing pattern).

### Step 5.6: Commit

- [ ] Run:
```bash
git add backend/internal/backup/restore.go backend/internal/api/admin.go
git commit -m "feat(backup): restore saved articles with article-id remap"
```

---

## Task 6: Prune deletes sibling and sweeps orphans

**Files:**
- Modify: `backend/internal/backup/retention.go`
- Modify: `backend/internal/backup/retention_test.go`

### Step 6.1: Write the failing tests

- [ ] Append to `backend/internal/backup/retention_test.go`:

```go
// makeFilesWithSaved drops one backup pair (metadata + saved sibling) per
// timestamp into a fresh temp dir.
func makeFilesWithSaved(t *testing.T, times []time.Time) string {
	t.Helper()
	dir := t.TempDir()
	for _, ts := range times {
		name := fileNamePrefix + ts.UTC().Format(fileTimeLayout) + fileNameSuffix
		metaPath := filepath.Join(dir, name)
		if err := os.WriteFile(metaPath, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		savedPath := savedSiblingPath(metaPath)
		if err := os.WriteFile(savedPath, []byte("dummy gz"), 0o644); err != nil {
			t.Fatalf("write %s: %v", savedPath, err)
		}
	}
	return dir
}

func TestPruneDeletesSibling(t *testing.T) {
	// Two timestamps in the same monthly bucket — prune should leave 1 pair.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	old := []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
	}
	dir := makeFilesWithSaved(t, old)

	if _, err := Prune(dir, now, DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	jsonCount, gzCount := 0, 0
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".saved.json.gz"):
			gzCount++
		case strings.HasSuffix(e.Name(), ".json"):
			jsonCount++
		}
	}
	if jsonCount != 1 || gzCount != 1 {
		t.Errorf("expected 1 metadata + 1 sibling, got json=%d gz=%d", jsonCount, gzCount)
	}
}

func TestPruneSweepsOrphanSaved(t *testing.T) {
	// Orphan saved.json.gz with no matching metadata — Prune deletes it.
	dir := t.TempDir()
	orphan := filepath.Join(dir, "rss-pal-backup-20260101-000000.saved.json.gz")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(dir, time.Now(), DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("expected orphan deleted, stat err = %v", err)
	}
}

func TestPruneKeepsSiblingWhenMetadataKept(t *testing.T) {
	// Single recent pair — neither file should be removed.
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	dir := makeFilesWithSaved(t, []time.Time{now.Add(-1 * time.Hour)})

	if _, err := Prune(dir, now, DefaultRetention); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("expected pair preserved, got %d entries", len(entries))
	}
}
```

Make sure the `strings` import is present at the top of the file:

```go
import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)
```

### Step 6.2: Run the tests — expect fail

- [ ] Run:
```bash
cd backend && go test ./internal/backup/ -run "TestPruneDeletesSibling|TestPruneSweepsOrphanSaved|TestPruneKeepsSiblingWhenMetadataKept" -v
```
Expected: `TestPruneDeletesSibling` and `TestPruneSweepsOrphanSaved` FAIL (sibling files remain / orphan untouched). `TestPruneKeepsSiblingWhenMetadataKept` likely passes by luck.

### Step 6.3: Update Prune in retention.go

- [ ] Replace the body of `Prune` in `backend/internal/backup/retention.go`:

```go
func Prune(dir string, now time.Time, policy RetentionPolicy) ([]string, error) {
	// First: sweep any orphan saved-archive files (no matching metadata).
	// They can result from a crash between WriteSavedFile and WriteFile, or
	// from a half-finished prune.
	if err := sweepOrphanSavedFiles(dir); err != nil {
		return nil, err
	}

	files, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool { return files[i].CreatedAt.After(files[j].CreatedAt) })

	keep := map[string]bool{files[0].Name: true}
	seenBucket := map[string]bool{}

	for _, f := range files {
		age := now.Sub(f.CreatedAt)
		switch {
		case age < policy.KeepAllWithin:
			keep[f.Name] = true
		case age < policy.WeeklyUntil:
			y, w := f.CreatedAt.ISOWeek()
			key := fmt.Sprintf("w-%04d-%02d", y, w)
			if !seenBucket[key] {
				seenBucket[key] = true
				keep[f.Name] = true
			}
		default:
			key := fmt.Sprintf("m-%04d-%02d", f.CreatedAt.Year(), int(f.CreatedAt.Month()))
			if !seenBucket[key] {
				seenBucket[key] = true
				keep[f.Name] = true
			}
		}
	}

	var removed []string
	for _, f := range files {
		if keep[f.Name] {
			continue
		}
		metaPath := filepath.Join(dir, f.Name)
		if err := os.Remove(metaPath); err != nil {
			return removed, fmt.Errorf("remove %s: %w", f.Name, err)
		}
		removed = append(removed, f.Name)
		// Best-effort: delete the saved sibling. Missing sibling (legacy
		// backup) is fine.
		savedPath := savedSiblingPath(metaPath)
		if err := os.Remove(savedPath); err != nil && !os.IsNotExist(err) {
			// Log via the typical convention used in this package would be
			// nice, but Prune currently doesn't import "log". Return
			// removed-so-far and the error so the caller (trigger.go) logs.
			return removed, fmt.Errorf("remove sibling %s: %w", filepath.Base(savedPath), err)
		}
	}
	return removed, nil
}

// sweepOrphanSavedFiles deletes any *.saved.json.gz whose paired metadata
// file is absent. Called at the top of Prune.
func sweepOrphanSavedFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Index existing metadata filenames so we can check siblings in O(1).
	hasMeta := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := parseFilename(e.Name()); ok {
			hasMeta[e.Name()] = true
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, savedFileSuffix) {
			continue
		}
		// Reconstruct expected metadata filename: strip .saved.json.gz, append .json.
		metaName := strings.TrimSuffix(name, savedFileSuffix) + fileNameSuffix
		if hasMeta[metaName] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("sweep orphan %s: %w", name, err)
		}
	}
	return nil
}
```

Add `"strings"` to the imports of `retention.go` if not already present.

### Step 6.4: Run the tests — expect pass

- [ ] Run:
```bash
cd backend && go test ./internal/backup/... -v
```
Expected: all PASS, including the three new prune tests and all existing ones.

### Step 6.5: Commit

- [ ] Run:
```bash
git add backend/internal/backup/retention.go backend/internal/backup/retention_test.go
git commit -m "feat(backup): prune deletes sibling and sweeps orphan saved files"
```

---

## Task 7: Bookmarklet capture triggers debounced backup

**Files:**
- Modify: `backend/internal/api/bookmarklet.go`
- Modify: `backend/cmd/server/main.go`

### Step 7.1: Add backup runner to BookmarkletHandler

- [ ] In `backend/internal/api/bookmarklet.go`, find the `BookmarkletHandler` struct (around line 65) and add the field. Find:

```go
type BookmarkletHandler struct {
```

and the existing fields below it. After the closing `}` of the struct, the existing `NewBookmarkletHandler` ends. Modify:

1. Add `backup *backup.Runner` field to the struct.
2. Add `"github.com/bytedance/rss-pal/internal/backup"` to the imports if not already there.
3. Add a `WithBackupRunner` setter mirroring FeedHandler's pattern.

Apply with this Edit:

Find:
```go
type BookmarkletHandler struct {
	userRepo    *repository.UserRepository
	feedRepo    *repository.FeedRepository
	articleRepo *repository.ArticleRepository
}

func NewBookmarkletHandler(
	userRepo *repository.UserRepository,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *BookmarkletHandler {
	return &BookmarkletHandler{
		userRepo:    userRepo,
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
	}
}
```

(Field set may be slightly different — preserve it; only add the `backup` field and `WithBackupRunner`.) Replace with:

```go
type BookmarkletHandler struct {
	userRepo    *repository.UserRepository
	feedRepo    *repository.FeedRepository
	articleRepo *repository.ArticleRepository
	backup      *backup.Runner // nil when backup is disabled
}

func NewBookmarkletHandler(
	userRepo *repository.UserRepository,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *BookmarkletHandler {
	return &BookmarkletHandler{
		userRepo:    userRepo,
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
	}
}

// WithBackupRunner wires a backup runner so successful captures trigger a
// debounced snapshot. Pass nil to disable.
func (h *BookmarkletHandler) WithBackupRunner(r *backup.Runner) *BookmarkletHandler {
	h.backup = r
	return h
}
```

Add the import:
```go
"github.com/bytedance/rss-pal/internal/backup"
```

### Step 7.2: Call TriggerAsync at the two success points

- [ ] In `bookmarklet.go`, find the "updated article" success branch (around line 195, just before `c.JSON(http.StatusOK, gin.H{"status": "updated", ...})`). Add `triggerBackup(h)`.

Find:
```go
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + title,
		})
		return
```

Replace with:
```go
		if h.backup != nil {
			h.backup.TriggerAsync()
		}
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + title,
		})
		return
```

Then find the "created article" success branch (around line 223):
```go
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"article_id": article.ID,
		"message":    "已加入网摘: " + title,
	})
```

Replace with:
```go
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	if h.backup != nil {
		h.backup.TriggerAsync()
	}
	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"article_id": article.ID,
		"message":    "已加入网摘: " + title,
	})
```

### Step 7.3: Wire backup runner in server main

- [ ] In `backend/cmd/server/main.go`, find:

```go
	bookmarkletHandler := api.NewBookmarkletHandler(userRepo, feedRepo, articleRepo)
```

Replace with:
```go
	bookmarkletHandler := api.NewBookmarkletHandler(userRepo, feedRepo, articleRepo).WithBackupRunner(backupRunner)
```

### Step 7.4: Verify build

- [ ] Run:
```bash
cd backend && go build ./...
```
Expected: clean build.

### Step 7.5: Run full test suite

- [ ] Run:
```bash
cd backend && go test ./... 2>&1 | tail -30
```
Expected: no test failures introduced. (Some packages have no tests — that's fine.)

### Step 7.6: Commit

- [ ] Run:
```bash
git add backend/internal/api/bookmarklet.go backend/cmd/server/main.go
git commit -m "feat(backup): bookmarklet capture triggers debounced snapshot"
```

---

## Task 8: Manual verification

This step has no automated test — the saved snapshot's correctness against a real DB is verified by hand.

- [ ] **Step 8.1: Rebuild and start the stack.**
```bash
docker-compose up -d --build api worker
```
Wait for `docker-compose logs --tail=20 api` to show server ready.

- [ ] **Step 8.2: Capture an article via the bookmarklet** so there's at least one 网摘 article in the DB.

- [ ] **Step 8.3: Trigger an immediate backup** via the admin "back up now" button on the Settings page (or POST `/api/admin/backups/now`).

- [ ] **Step 8.4: Verify both files were written.**
```bash
docker-compose exec api ls -la "$BACKUP_DIR" | grep -E "$(date +%Y%m%d)"
```
Expected: see one `rss-pal-backup-...json` and one `rss-pal-backup-....saved.json.gz` with the same timestamp prefix.

- [ ] **Step 8.5: Inspect the saved file content.**
```bash
docker-compose exec api sh -c 'gunzip -c "$BACKUP_DIR"/rss-pal-backup-*.saved.json.gz | head -80'
```
Expected: a JSON document with `version: 1`, a `saved_articles` array containing at least the article you captured, and the article's `content` field populated.

- [ ] **Step 8.6: Test restore on a fresh DB.** This is destructive — only do this in a dev environment.

Stop services, drop volume, restart with migrations, then restore:
```bash
docker-compose down
docker volume rm rss-pal_postgres-data 2>/dev/null || true
docker-compose up -d postgres
# wait for migrations
sleep 5
# Restore manually by re-uploading the backup dir or via the admin endpoint.
# Then re-start api/worker.
```

After restore, verify in the UI that the previously-captured 网摘 article is present with its original content, and any tags / save signals attached to it are restored.

- [ ] **Step 8.7: Verify legacy backup still loads.** Take a pre-existing `.json` file (without a sibling), place it in the backup dir, click restore. Expect: success, with stats showing `saved_articles: 0` and existing relations under `skipped_article_link`.

---

## Self-Review

**Spec coverage:**
- [x] File layout (§1) → Task 4 (WriteFiles), Task 6 (Prune)
- [x] Write order ②→① → Task 4
- [x] LoadSaved tolerates missing sibling → Task 2
- [x] Prune deletes sibling + sweeps orphans → Task 6
- [x] SavedSnapshot schema → Task 1, Task 2
- [x] Saved predicate query → Task 3
- [x] ReadingProgress filtered by saved IDs → Task 3
- [x] Build returns both → Task 3
- [x] Restore article-id mapping → Task 5
- [x] RestoreStats new fields → Task 5
- [x] Conflict semantics (DO NOTHING) → Task 5
- [x] Bookmarklet TriggerAsync → Task 7
- [x] Versioning rejects unknown future version → Task 2
- [x] Tests (1–9 in spec) → covered across Tasks 1, 2, 4, 5, 6; DB-touching cases (#1, #2, #5, #6, #7) are verified manually in Task 8 (matches existing module's no-DB-test pattern)

**Placeholder scan:** No TODO/TBD/"handle edge cases" markers — all code blocks are complete.

**Type consistency:** `Build`, `WriteFiles`, `Restore`, `RestoreStats`, `SavedSnapshot`, `SavedArticleRow`, `ReadingProgressRow`, `LoadSaved`, `WriteSavedFile`, `savedSiblingPath`, `sweepOrphanSavedFiles` — names match across tasks.

**Known caveat verified in Task 5.2:** The `ON CONFLICT (article_id) DO NOTHING` for reading_progress assumes the unique constraint on that column. Task 5.2 explicitly checks and instructs the implementer to adjust if the constraint differs.
