package explore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func seedContentRepair(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	userID := seedSubscribeUser(t, db, "repair")
	sourceID := seedSubscribeSource(t, db, userID, "https://repair.example/feed", "Repair", "valid", time.Now().UTC())
	var exploreID, feedID, articleID int
	err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content) VALUES($1,'https://repair.example/post','https://repair.example/post','Title','<p>Hello <strong>world</strong></p>') RETURNING id`, sourceID).Scan(&exploreID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://repair.example/feed','Repair',$1) RETURNING id`, userID).Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,word_count) VALUES($1,'Title','https://repair.example/post','<p>Hello <strong>world</strong></p>','old brief','old detailed',99) RETURNING id`, feedID).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	return exploreID, articleID
}

func TestRepairContentDryRunApplyAndRepeat(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	exploreID, articleID := seedContentRepair(t, db)
	// An independently refreshed copy and a clipped article must be preserved.
	if _, err := db.Exec(`INSERT INTO articles(feed_id,title,url,content,is_clip) SELECT feed_id,'Clip',url,content,true FROM articles WHERE id=$1`, articleID); err != nil {
		t.Fatal(err)
	}
	stats, err := RepairContent(context.Background(), db, ContentRepairOptions{})
	if err != nil || stats.Legacy != 1 || stats.Changed != 1 || stats.Articles != 1 {
		t.Fatalf("dry=%+v err=%v", stats, err)
	}
	var got string
	var version int
	if err := db.QueryRow(`SELECT content,content_version FROM explore_articles WHERE id=$1`, exploreID).Scan(&got, &version); err != nil {
		t.Fatal(err)
	}
	if version != 0 || !strings.Contains(got, "<p>") {
		t.Fatal("dry run wrote data")
	}
	backups := 0
	stats, err = RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(b ContentRepairBackup) error {
		backups++
		if !strings.Contains(string(b.Explore), "<p>") || len(b.Articles) != 1 || b.NewContent.String != "Hello **world**" {
			t.Fatalf("backup=%+v", b)
		}
		return nil
	}})
	if err != nil || backups != 1 || stats.Articles != 1 {
		t.Fatalf("apply=%+v backups=%d err=%v", stats, backups, err)
	}
	if err := db.QueryRow(`SELECT content,content_version FROM explore_articles WHERE id=$1`, exploreID).Scan(&got, &version); err != nil {
		t.Fatal(err)
	}
	if got != "Hello **world**" || version != 1 {
		t.Fatalf("cache=%q version=%d", got, version)
	}
	var words int
	var brief sql.NullString
	if err := db.QueryRow(`SELECT content,word_count,summary_brief FROM articles WHERE id=$1`, articleID).Scan(&got, &words, &brief); err != nil {
		t.Fatal(err)
	}
	if got != "Hello **world**" || words != 2 || brief.Valid {
		t.Fatalf("article=%q words=%d summary=%v", got, words, brief)
	}
	stats, err = RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(ContentRepairBackup) error { t.Fatal("repeat wrote backup"); return nil }})
	if err != nil || stats.Legacy != 0 || stats.Changed != 0 || stats.Articles != 0 {
		t.Fatalf("repeat=%+v err=%v", stats, err)
	}
}

func TestRepairContentBackupFailureRollsBack(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	exploreID, articleID := seedContentRepair(t, db)
	fail := errors.New("backup failed")
	_, err := RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(ContentRepairBackup) error { return fail }})
	if !errors.Is(err, fail) {
		t.Fatalf("err=%v", err)
	}
	var version int
	var content string
	if err := db.QueryRow(`SELECT content_version FROM explore_articles WHERE id=$1`, exploreID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT content FROM articles WHERE id=$1`, articleID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if version != 0 || !strings.Contains(content, "<p>") {
		t.Fatal("partial write on failed backup")
	}
}

func TestRepairContentHandlesRefreshedCacheAndPreservesIndependentBody(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	exploreID, articleID := seedContentRepair(t, db)
	// A fresh fetch may normalize cache before the repair reaches it.
	if _, err := db.Exec(`UPDATE explore_articles SET legacy_content_hash=md5(content),content='Hello **world**',content_version=1 WHERE id=$1`, exploreID); err != nil {
		t.Fatal(err)
	}
	stats, err := RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(ContentRepairBackup) error { return nil }})
	if err != nil || stats.Legacy != 0 || stats.Articles != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if _, err := db.Exec(`UPDATE articles SET content='<p>Independently fetched longer body</p>' WHERE id=$1`, articleID); err != nil {
		t.Fatal(err)
	}
	stats, err = RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(ContentRepairBackup) error { t.Fatal("unexpected write"); return nil }})
	if err != nil || stats.Articles != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestRepairContentPreservesEquivalentButNotExactImport(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	_, articleID := seedContentRepair(t, db)
	if _, err := db.Exec(`UPDATE articles SET content='<p class="independent">Hello <strong>world</strong></p>' WHERE id=$1`, articleID); err != nil {
		t.Fatal(err)
	}
	stats, err := RepairContent(context.Background(), db, ContentRepairOptions{Apply: true, Backup: func(ContentRepairBackup) error { return nil }})
	if err != nil || stats.Articles != 0 {
		t.Fatalf("independent body changed: %+v err=%v", stats, err)
	}
}
