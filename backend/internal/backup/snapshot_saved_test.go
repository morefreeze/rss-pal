package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
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
	got, err := LoadSaved(metaPath)
	if err != nil {
		t.Fatalf("LoadSaved on missing sibling: unexpected err %v", err)
	}
	if got != nil {
		t.Errorf("LoadSaved on missing sibling: want nil, got %+v", got)
	}
}

func TestWriteSavedFileAtomic(t *testing.T) {
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

func TestLoadSavedRejectsFutureVersion(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "rss-pal-backup-20260514-093015.json")

	// Write a saved sibling whose Version is one beyond what this build supports.
	ss := &SavedSnapshot{
		Version:   SavedSnapshotVersion + 1,
		CreatedAt: time.Date(2026, 5, 14, 9, 30, 15, 0, time.UTC),
	}
	if err := WriteSavedFile(ss, metaPath); err != nil {
		t.Fatalf("WriteSavedFile: %v", err)
	}

	got, err := LoadSaved(metaPath)
	if err == nil {
		t.Fatalf("LoadSaved on future version: expected error, got snapshot %+v", got)
	}
	if got != nil {
		t.Errorf("LoadSaved on future version: want nil snapshot, got %+v", got)
	}
}

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
		{
			metadata: "rss-pal-backup-20260514", // no .json suffix → fallback path
			want:     "rss-pal-backup-20260514.saved.json.gz",
		},
	}
	for _, c := range cases {
		got := savedSiblingPath(c.metadata)
		if got != c.want {
			t.Errorf("savedSiblingPath(%q) = %q, want %q", c.metadata, got, c.want)
		}
	}
}

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
			ExportID:  1,
			FeedURL:   "bookmarklet://user/7",
			Title:     "x",
			URL:       "https://example.com/a",
			Content:   "body",
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
