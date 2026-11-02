package backup

import (
	"context"
	"encoding/json"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
)

func TestSummaryGenerationSurvivesSavedBackupAndRestore(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var user, feed, parent, clip, child int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('provenance-backup','x') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,owner_id) VALUES('https://provenance.example/feed',$1) RETURNING id`, user).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,summary_generation) VALUES($1,'Title','https://provenance.example/root','body','brief','details','{"policy_version":"test-policy"}') RETURNING id`, feed).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,summary_generation,is_clip) VALUES($1,'Clip','https://provenance.example/clip','body','brief','details','{"policy_version":"test-policy"}',true) RETURNING id`, feed).Scan(&clip); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,summary_generation,parent_article_id) VALUES($1,'Child','https://provenance.example/child','body','brief','details','{"policy_version":"test-policy"}',$2) RETURNING id`, feed, parent).Scan(&child); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{parent, clip, child} {
		if _, err := db.Exec(`INSERT INTO article_capture_notes(user_id,article_id,capture_note) VALUES($1,$2,'note')`, user, id); err != nil {
			t.Fatal(err)
		}
	}
	meta, saved, err := Build(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.SavedArticles) != 3 {
		t.Fatalf("articles %d", len(saved.SavedArticles))
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SavedSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, article := range decoded.SavedArticles {
		if len(article.SummaryGeneration) == 0 {
			t.Fatal("backup lost generation")
		}
	}
	if _, err := db.Exec(`DELETE FROM articles`); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), db, meta, &decoded); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE summary_generation->>'policy_version'='test-policy'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("restored %d %v", count, err)
	}
}
