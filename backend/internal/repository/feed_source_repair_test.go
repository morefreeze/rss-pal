package repository

import (
	"os"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRepairLegacyFeedSources(t *testing.T) {
	for _, withArticle := range []bool{false, true} {
		name := "empty_duplicate"
		if withArticle {
			name = "protect_duplicate_articles"
		}
		t.Run(name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			_, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES (1,'repair-owner','x'),(2,'other-owner','x');
INSERT INTO feeds(id,owner_id,url,title,feed_type) VALUES
(27,1,'bookmarklet://user/1','⭐ 网摘','clip'),
(45,1,'https://extension://twitter:user/1/alibaba_qwen','','rss'),
(46,1,'https://extension://twitter:user/1/fred17304866704','','rss'),
(47,1,'https://extension://twitter:user/1/zarazhangrui','','rss'),
(48,1,'https://extension://twitter:user/1/zhang_benita','','rss'),
(49,1,'https://extension://twitter:bookmarks/1/self','','rss'),
(59,1,'https://buttondown.com/hacker-newsletter/archive/793/','','rss'),
(61,1,'https://bookmarklet://user/1','','rss'),
(99,2,'https://other.example/rss','Other owner','rss');
INSERT INTO articles(feed_id,title,url) VALUES (27,'Saved clip','https://example.com/saved');`)
			if err != nil {
				t.Fatal(err)
			}
			if withArticle {
				if _, err := db.Exec(`INSERT INTO articles(feed_id,title,url) VALUES (61,'Must preserve','https://example.com/duplicate')`); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile("../../../scripts/repair-feed-sources-20261022.sql")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(string(data))
			if withArticle {
				if err == nil {
					t.Fatal("repair must refuse a duplicate with articles")
				}
				var count int
				if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE id IN (45,46,47,48,49,59,61) AND feed_type='rss'`).Scan(&count); err != nil || count != 7 {
					t.Fatalf("repair was not atomic: count=%d err=%v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(data)); err != nil {
				t.Fatalf("second run: %v", err)
			}
			var corrected int
			if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE id IN (45,46,47,48) AND feed_type='twitter:user' AND url='extension://twitter:user/1/'||provider_source_id AND title='X 用户 · @'||provider_source_id`).Scan(&corrected); err != nil || corrected != 4 {
				t.Fatalf("X users: count=%d err=%v", corrected, err)
			}
			var valid bool
			if err := db.QueryRow(`SELECT
EXISTS(SELECT 1 FROM feeds WHERE id=49 AND url='extension://twitter:bookmarks/1/self' AND feed_type='twitter:bookmarks' AND provider_source_id='self' AND title='X 书签')
AND EXISTS(SELECT 1 FROM feeds WHERE id=59 AND feed_type='html' AND title='Hacker Newsletter · 第 793 期')
AND NOT EXISTS(SELECT 1 FROM feeds WHERE id=61)
AND (SELECT count(*) FROM feeds WHERE owner_id=1 AND feed_type='clip')=1
AND EXISTS(SELECT 1 FROM articles WHERE feed_id=27 AND title='Saved clip')
AND EXISTS(SELECT 1 FROM feeds WHERE id=99 AND owner_id=2 AND title='Other owner')`).Scan(&valid); err != nil || !valid {
				t.Fatalf("repair invariants: valid=%t err=%v", valid, err)
			}
		})
	}
}
