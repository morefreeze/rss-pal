package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreRelatedProducerPersistsBoundedCursorsAndCanonicalTasks(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userID int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('related-producer','x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		raw := fmt.Sprintf("https://seed-%d.example/post?utm_source=%d", i%3, i)
		if _, err := db.Exec(`INSERT INTO feeds(url,title,owner_id,status,is_active) VALUES ($1,$2,$3,'active',true)`, raw, raw, userID); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewExploreRelatedTaskRepository(db)
	first, err := repo.Produce(context.Background(), time.Now(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if first.ScannedFeeds != 4 || first.ScannedArticles != 0 || first.Enqueued != 3 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := repo.Produce(context.Background(), time.Now().Add(time.Minute), 4)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScannedFeeds != 3 {
		t.Fatalf("second page = %+v", second)
	}
	var feedCursor int
	if err := db.QueryRow(`SELECT feed_cursor FROM explore_related_scan_state WHERE id=1`).Scan(&feedCursor); err != nil {
		t.Fatal(err)
	}
	if feedCursor == 0 {
		t.Fatal("feed cursor was not persisted")
	}
	var active int
	if err := db.QueryRow(`SELECT count(*) FROM explore_related_tasks WHERE status='pending'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 3 {
		t.Fatalf("canonical active tasks=%d, want 3", active)
	}
	wrapped, err := repo.Produce(context.Background(), time.Now().Add(2*time.Minute), 4)
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.ScannedFeeds != 4 {
		t.Fatalf("wrapped page=%+v", wrapped)
	}
}

func TestExploreRelatedProducerEventuallyPassesLargeDuplicatePrefix(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userID int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('related-fairness','x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 260; i++ {
		raw := fmt.Sprintf("https://duplicate.example/post?utm_source=%d", i)
		if i == 259 {
			raw = "https://eventual.example/unique"
		}
		if _, err := db.Exec(`INSERT INTO feeds(url,title,owner_id,status,is_active) VALUES ($1,$2,$3,'active',true)`, raw, raw, userID); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewExploreRelatedTaskRepository(db)
	for cycle := 0; cycle < 3; cycle++ {
		if _, err := repo.Produce(context.Background(), time.Now().Add(time.Duration(cycle)*time.Minute), 100); err != nil {
			t.Fatal(err)
		}
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM explore_related_tasks WHERE canonical_seed_url='https://eventual.example/unique')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("bounded cursor never reached unique seed after duplicate prefix")
	}
}
