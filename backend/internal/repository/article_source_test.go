package repository

import (
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestFetchedLinkSourceAndParentProvenance(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(1,'reader','x'),(2,'other','x')`); err != nil {
		t.Fatal(err)
	}
	var feed, parent, child int
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://ruanyifeng.com/feed','阮一峰的网络日志',1) RETURNING id`).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content) VALUES($1,'科技爱好者周刊（第 415 期）','https://ruanyifeng.com/415','') RETURNING id`, feed).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	repo := NewArticleRepository(db)
	if _, err := repo.EnableAndInsertLinkSetChildren(parent, []LinkSetChildInput{{FeedID: feed, ParentArticleID: parent, Title: "DeskMind", URL: "https://deskmind.dev/zh/blog/launch", ProcessingState: "ready"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM articles WHERE parent_article_id=$1`, parent).Scan(&child); err != nil {
		t.Fatal(err)
	}
	a, _, err := repo.GetByIDWithFeedType(child, 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Source == nil || a.Source.Title != "deskmind.dev" {
		t.Fatalf("fetched child source=%+v", a.Source)
	}
	if a.ParentTitle != "科技爱好者周刊（第 415 期）" {
		t.Fatalf("parent title=%q", a.ParentTitle)
	}
	tag, err := NewArticleUserTagRepository(db).GetSourceForArticle(child, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Title != "deskmind.dev" {
		t.Fatalf("source tag=%q", tag.Title)
	}
	if _, _, err := repo.GetByIDWithFeedType(child, 2); err == nil {
		t.Fatal("other user accessed child")
	}
	if _, err := NewArticleUserTagRepository(db).GetSourceForArticle(child, 2); err == nil {
		t.Fatal("other user accessed source")
	}
	legacy, err := repo.GetByID(child, 1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Source == nil || legacy.Source.Title != "deskmind.dev" {
		t.Fatalf("legacy source=%+v", legacy.Source)
	}
	snapshot := SnapshotFromArticle(legacy, time.Now())
	if snapshot.FeedTitle != "deskmind.dev" {
		t.Fatalf("public source=%q", snapshot.FeedTitle)
	}
	children, err := repo.GetVisibleChildren(parent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Source == nil || children[0].Source.Title != "deskmind.dev" {
		t.Fatalf("children=%+v", children)
	}
}
