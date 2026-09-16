package repository

import (
	"fmt"
	"os"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestUserSubscriptionsZeroByDefaultAndLifecycleIndependent(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	newUser := seedOwnerScopedFeedUser(t, db, "empty-subscriptions")
	otherUser := seedOwnerScopedFeedUser(t, db, "existing-subscriber")
	if _, err := db.Exec(`INSERT INTO feeds(url,owner_id) VALUES('https://private', $1),('https://unowned',NULL)`, otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO articles(feed_id,url,title) SELECT id,url||'/post','Post' FROM feeds; INSERT INTO recommended_feeds(url,normalized_url,title,category,language) VALUES('https://catalog','https://catalog','Catalog','ai_eng','en')`); err != nil {
		t.Fatal(err)
	}
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	tx, err := app.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('app.user_id',$1,true)`, fmt.Sprint(newUser)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"feeds", "articles"} {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("new user %s=%d err=%v", table, n, err)
		}
	}
	var catalog int
	if err := tx.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE url='https://catalog'`).Scan(&catalog); err != nil || catalog != 1 {
		t.Fatalf("public catalog=%d err=%v", catalog, err)
	}

	feeds, err := NewFeedRepository(db).GetVisibleByUser(newUser)
	if err != nil || len(feeds) != 0 {
		t.Fatalf("new user feeds=%v err=%v", feeds, err)
	}
	stats, err := NewStatsRepository(db).GetStats(newUser)
	if err != nil || stats.TotalFeeds != 0 || stats.TotalArticles != 0 {
		t.Fatalf("new user stats=%+v err=%v", stats, err)
	}
}

func TestSubscriptionMigrationPreservesLegacyIDsAndPausedState(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "043_task_budgets.sql")
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO users (id,username,password_hash,is_admin) VALUES (1,'admin','x',true),(2,'reader','x',false);
 INSERT INTO feeds (url,title,status,is_active) SELECT 'https://legacy/'||n,'Legacy',CASE WHEN n<=2 THEN 'paused' ELSE 'active' END,n>2 FROM generate_series(1,5) n;
 INSERT INTO articles (feed_id,url,title) SELECT id,url||'/post','Post' FROM feeds;
 INSERT INTO reading_progress (user_id,article_id) SELECT 1,id FROM articles;`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/044_user_subscriptions.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var owned, paused, articles, progress int
	if err := db.QueryRow(`SELECT count(*),count(*) FILTER (WHERE status='paused' AND NOT is_active) FROM feeds WHERE owner_id=1`).Scan(&owned, &paused); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE id=feed_id`).Scan(&articles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM reading_progress WHERE user_id=1`).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if owned != 5 || paused != 2 || articles != 5 || progress != 5 {
		t.Fatalf("owned=%d paused=%d preserved articles=%d progress=%d", owned, paused, articles, progress)
	}
	feeds, err := NewFeedRepository(db).GetVisibleByUser(2)
	if err != nil || len(feeds) != 0 {
		t.Fatalf("reader inherited feeds=%v err=%v", feeds, err)
	}
}

func TestSubscriptionMigrationConflictsAbortWithoutChanges(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "043_task_budgets.sql")
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO users (id,username,password_hash,is_admin) VALUES (1,'admin','x',true); INSERT INTO feeds(url,owner_id) VALUES ('https://duplicate',NULL),('https://duplicate',1)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/044_user_subscriptions.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err == nil {
		t.Fatal("expected explicit conflict failure")
	}
	if _, err := db.Exec(`ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id IS NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("legacy count=%d err=%v", count, err)
	}
}

func TestSubscriptionMigrationRequiresLegacyAdministrator(t *testing.T) {
	for _, regularUser := range []bool{false, true} {
		t.Run(fmt.Sprint(regularUser), func(t *testing.T) {
			db, cleanup := testdb.NewThroughMigration(t, "043_task_budgets.sql")
			defer cleanup()
			if regularUser {
				if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,is_admin) VALUES(1,'ordinary','x',false)`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO feeds(url,title) VALUES('https://legacy','Legacy')`); err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../migrations/044_user_subscriptions.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(migration)); err == nil {
				t.Fatal("migration accepted missing administrator")
			}
			if _, err := db.Exec(`ROLLBACK`); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id IS NULL`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("legacy count=%d err=%v", n, err)
			}
		})
	}
}
