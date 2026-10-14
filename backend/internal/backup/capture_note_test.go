package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestCaptureNotesBackupRestoreByOwner(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	for i := 0; i < 2; i++ {
		var uid, fid, aid int
		if err := db.QueryRow(`INSERT INTO users (username,password_hash) VALUES ($1,'x') RETURNING id`, fmt.Sprintf("note-user-%d", i)).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO feeds (url,title,owner_id) VALUES ('https://same.example/feed','feed',$1) RETURNING id`, uid).Scan(&fid); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO articles (feed_id,url,title,content) VALUES ($1,'https://same.example/article','title','body') RETURNING id`, fid).Scan(&aid); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)`, uid, aid, fmt.Sprintf("private note %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	s, ss, err := Build(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ss)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.SavedArticles) != 2 || !strings.Contains(string(raw), `"capture_notes"`) || !strings.Contains(string(raw), "private note 1") {
		t.Fatalf("notes or annotated articles missing from backup: %s", raw)
	}
	// Exercise the JSON contract, not only in-memory structs.
	var decoded SavedSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM articles`); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), db, s, &decoded); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_notes n JOIN articles a ON a.id=n.article_id JOIN feeds f ON f.id=a.feed_id WHERE n.user_id=f.owner_id`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restored notes not bound to owner: %d %v", count, err)
	}
	if _, err := db.Exec(`UPDATE article_capture_notes SET capture_note='existing wins'`); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), db, s, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_notes WHERE capture_note='existing wins'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restore overwrote existing notes: %d %v", count, err)
	}
}

func TestCaptureNotesBackupPreservesDistinctChildren(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var uid, fid int
	if err := db.QueryRow(`INSERT INTO users (username,password_hash) VALUES ('child-owner','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds (url,title,owner_id) VALUES ('https://child.example/feed','feed',$1) RETURNING id`, uid).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var parent, child int
		if err := db.QueryRow(`INSERT INTO articles (feed_id,url,title,content,links_extendable) VALUES ($1,$2,'parent','parent body',true) RETURNING id`, fid, fmt.Sprintf("https://child.example/parent-%d", i)).Scan(&parent); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO articles (feed_id,url,title,content,parent_article_id) VALUES ($1,'https://same.example/child','child',$2,$3) RETURNING id`, fid, fmt.Sprintf("child body %d", i), parent).Scan(&child); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)`, uid, child, fmt.Sprintf("child note %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	s, ss, err := Build(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ss)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SavedSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// Reverse the wire order to ensure restore resolves parent identities,
	// rather than relying on original IDs or array ordering.
	for i, j := 0, len(decoded.SavedArticles)-1; i < j; i, j = i+1, j-1 {
		decoded.SavedArticles[i], decoded.SavedArticles[j] = decoded.SavedArticles[j], decoded.SavedArticles[i]
	}
	if _, err := db.Exec(`DELETE FROM articles`); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), db, s, &decoded); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_notes n JOIN articles a ON a.id=n.article_id JOIN articles p ON p.id=a.parent_article_id WHERE a.content=replace(n.capture_note,'note','body') AND p.links_extendable`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("distinct child notes/parents lost: count=%d err=%v snapshot=%s", count, err, raw)
	}
	if _, err := Restore(context.Background(), db, s, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_notes`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("child restore duplicated notes: count=%d err=%v", count, err)
	}
}
