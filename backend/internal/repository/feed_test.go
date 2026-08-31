package repository

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestOwnerScopedFeedSameOwnerIsIdempotentAndUsersStayIndependent(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userA := seedOwnerScopedFeedUser(t, db, "owner-feed-a")
	userB := seedOwnerScopedFeedUser(t, db, "owner-feed-b")
	repo := NewFeedRepository(db)

	first, created, err := repo.GetOrCreateOwnerScoped(userA, "https://example.com/feed.xml", "Example", "rss")
	if err != nil || !created {
		t.Fatalf("first=%+v created=%t err=%v", first, created, err)
	}
	again, created, err := repo.GetOrCreateOwnerScoped(userA, "https://example.com/feed.xml", "Changed", "rss")
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("again=%+v created=%t err=%v", again, created, err)
	}
	other, created, err := repo.GetOrCreateOwnerScoped(userB, "https://example.com/feed.xml", "Example", "rss")
	if err != nil || !created || other.ID == first.ID {
		t.Fatalf("other=%+v created=%t first=%+v err=%v", other, created, first, err)
	}
}

func TestOwnerScopedFeedReusesVisibleSharedFeed(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID := seedOwnerScopedFeedUser(t, db, "owner-feed-shared")
	var sharedID int
	if err := db.QueryRow(`INSERT INTO feeds (url,title) VALUES ('https://shared.example/feed','Shared') RETURNING id`).Scan(&sharedID); err != nil {
		t.Fatal(err)
	}
	feed, created, err := NewFeedRepository(db).GetOrCreateOwnerScoped(userID, "https://shared.example/feed", "Candidate", "rss")
	if err != nil || created || feed.ID != sharedID || feed.OwnerID != nil {
		t.Fatalf("feed=%+v created=%t err=%v", feed, created, err)
	}
	var owned int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1 AND url='https://shared.example/feed'`, userID).Scan(&owned); err != nil || owned != 0 {
		t.Fatalf("owned=%d err=%v", owned, err)
	}
}

func seedOwnerScopedFeedUser(t *testing.T, db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, username string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`INSERT INTO users (username,password_hash) VALUES ($1,'x') RETURNING id`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
