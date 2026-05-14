package backup

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
