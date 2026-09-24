package repository

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
)

func TestFeedCatalogMigrationPreservesPersonalSubscriptions(t *testing.T) {
	db, done := testdb.NewThroughMigration(t, "045_operations_monitoring.sql")
	defer done()
	_, err := db.Exec(`INSERT INTO users(id,username,password_hash,is_admin) VALUES(1,'admin','x',true),(2,'reader','x',false); INSERT INTO feeds(url,title,owner_id,is_active,status) VALUES('https://example.com/rss','Private',1,false,'paused'),('https://example.com/rss','Reader',2,true,'active')`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../migrations/046_public_feed_catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	var total, paused, public, unchecked int
	db.QueryRow(`SELECT count(*),count(*) FILTER(WHERE NOT is_active) FROM feeds`).Scan(&total, &paused)
	db.QueryRow(`SELECT count(*) FILTER(WHERE published),count(*) FILTER(WHERE check_status='unchecked') FROM public_feed_catalog`).Scan(&public, &unchecked)
	if total != 2 || paused != 1 || public != 0 || unchecked != 28 {
		t.Fatalf("feeds=%d paused=%d published=%d unchecked=%d", total, paused, public, unchecked)
	}
	if _, err = db.Exec(`UPDATE public_feed_catalog SET published=true WHERE id=1; UPDATE feeds SET is_active=true WHERE owner_id=1;DELETE FROM feeds WHERE owner_id=1`); err != nil {
		t.Fatal(err)
	}
	list, err := NewFeedCatalogRepository(db).List(context.Background(), true)
	if err != nil || len(list) != 1 {
		t.Fatalf("private feed changed catalog: %v %v", list, err)
	}
	var active bool
	if err = db.QueryRow(`SELECT is_active FROM feeds WHERE owner_id=2`).Scan(&active); err != nil || !active {
		t.Fatalf("reader changed: %v %v", active, err)
	}
}
