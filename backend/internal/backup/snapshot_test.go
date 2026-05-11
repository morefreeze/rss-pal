package backup

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestWriteFileAndLoad(t *testing.T) {
	dir := t.TempDir()
	owner := 7
	s := &Snapshot{
		Version:   SnapshotVersion,
		CreatedAt: time.Date(2026, 5, 11, 19, 23, 57, 0, time.UTC),
		Feeds: []model.Feed{
			{ID: 1, URL: "https://example.com/feed.xml", Title: "Example", OwnerID: &owner, FeedType: "rss", Status: "active", IsActive: true},
		},
		UserTags: []model.UserTag{
			{ID: 1, UserID: 7, Name: "reading", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	path, err := WriteFile(s, dir)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	expectedName := "rss-pal-backup-20260511-192357.json"
	if filepath.Base(path) != expectedName {
		t.Errorf("filename = %q, want %q", filepath.Base(path), expectedName)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != s.Version {
		t.Errorf("version: got %d, want %d", loaded.Version, s.Version)
	}
	if !loaded.CreatedAt.Equal(s.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", loaded.CreatedAt, s.CreatedAt)
	}
	if !reflect.DeepEqual(loaded.Feeds, s.Feeds) {
		t.Errorf("feeds roundtrip mismatch:\n got %+v\nwant %+v", loaded.Feeds, s.Feeds)
	}
	if !reflect.DeepEqual(loaded.UserTags, s.UserTags) {
		t.Errorf("user_tags roundtrip mismatch")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	// After WriteFile returns successfully, no .tmp leftover should remain.
	dir := t.TempDir()
	s := &Snapshot{Version: 1, CreatedAt: time.Now().UTC()}
	if _, err := WriteFile(s, dir); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected exactly 1 backup file, got %d", len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name, ".tmp") {
			t.Errorf("found leftover .tmp file: %s", f.Name)
		}
	}
}
