package backup

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRestoreFeedURLIsScopedByOwner(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var ownerA, ownerB int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('restore-owner-a', 'x') RETURNING id`).Scan(&ownerA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('restore-owner-b', 'x') RETURNING id`).Scan(&ownerB); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshot := &Snapshot{Feeds: []model.Feed{
		{URL: "https://restore.example/feed", Title: "A", OwnerID: &ownerA, FeedType: "rss", Status: "active", IsActive: true, CreatedAt: now},
		{URL: "https://restore.example/feed", Title: "B", OwnerID: &ownerB, FeedType: "rss", Status: "active", IsActive: true, CreatedAt: now},
	}}
	if _, err := Restore(context.Background(), db, snapshot, nil); err != nil {
		t.Fatalf("restore owner rows: %v", err)
	}
	if _, err := Restore(context.Background(), db, snapshot, nil); err != nil {
		t.Fatalf("restore owner idempotency: %v", err)
	}
	updatedOwnerA := &Snapshot{Feeds: []model.Feed{{
		URL: "https://restore.example/feed", Title: "A updated", OwnerID: &ownerA, FeedType: "rss", Status: "active", IsActive: true, CreatedAt: now,
	}}}
	if _, err := Restore(context.Background(), db, updatedOwnerA, nil); err != nil {
		t.Fatalf("restore same owner update: %v", err)
	}
	shared := &Snapshot{Feeds: []model.Feed{{
		URL: "https://restore.example/feed", Title: "shared", FeedType: "rss", Status: "active", IsActive: true, CreatedAt: now,
	}}}
	if _, err := Restore(context.Background(), db, shared, nil); err != nil {
		t.Fatalf("restore shared row: %v", err)
	}
	if _, err := Restore(context.Background(), db, shared, nil); err != nil {
		t.Fatalf("restore shared idempotency: %v", err)
	}

	var owned, sharedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE url = 'https://restore.example/feed' AND owner_id IS NOT NULL`).Scan(&owned); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE url = 'https://restore.example/feed' AND owner_id IS NULL`).Scan(&sharedCount); err != nil {
		t.Fatal(err)
	}
	if owned != 2 || sharedCount != 1 {
		t.Fatalf("owner-scoped restore rows: owned=%d shared=%d, want 2/1", owned, sharedCount)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM feeds WHERE url = 'https://restore.example/feed' AND owner_id = $1`, ownerA).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "A updated" {
		t.Fatalf("same-owner restore did not update conflict row: got %q", title)
	}
}
