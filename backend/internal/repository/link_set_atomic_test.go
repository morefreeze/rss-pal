package repository

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func seedLinkSetParent(t *testing.T) (*ArticleRepository, *sql.DB, int, int) {
	t.Helper()
	db, cleanup := testdb.New(t)
	t.Cleanup(cleanup)
	var feedID int
	if err := db.QueryRow(`INSERT INTO feeds (url, title) VALUES ('https://feed.example/atomic', 'Feed') RETURNING id`).Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	var parentID int
	if err := db.QueryRow(`INSERT INTO articles (feed_id, title, url, content) VALUES ($1, 'Parent', 'https://parent.example/atomic', '') RETURNING id`, feedID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	return NewArticleRepository(db), db, feedID, parentID
}

func TestEnableAndInsertLinkSetChildrenRollsBackPromotion(t *testing.T) {
	repo, db, feedID, parentID := seedLinkSetParent(t)
	_, err := repo.EnableAndInsertLinkSetChildren(parentID, []LinkSetChildInput{{
		FeedID:          feedID + 999999,
		ParentArticleID: parentID,
		Title:           "Bad child",
		URL:             "https://child.example/bad",
		ProcessingState: "processing",
	}})
	if err == nil {
		t.Fatal("expected foreign-key failure")
	}

	var enabled sql.NullBool
	if err := db.QueryRow(`SELECT links_extendable FROM articles WHERE id=$1`, parentID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled.Valid {
		t.Fatalf("promotion committed despite child failure: %+v", enabled)
	}
}

func TestEnableAndInsertLinkSetChildrenCommitsTogether(t *testing.T) {
	repo, db, feedID, parentID := seedLinkSetParent(t)
	inserted, err := repo.EnableAndInsertLinkSetChildren(parentID, []LinkSetChildInput{{
		FeedID:          feedID,
		ParentArticleID: parentID,
		Title:           "Child",
		URL:             "https://child.example/good",
		ProcessingState: "processing",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 1 {
		t.Fatalf("inserted=%d, want 1", inserted)
	}

	var enabled bool
	if err := db.QueryRow(`SELECT links_extendable FROM articles WHERE id=$1`, parentID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("parent was not promoted")
	}
	var children int
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, parentID).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if children != 1 {
		t.Fatalf("children=%d, want 1", children)
	}
}
