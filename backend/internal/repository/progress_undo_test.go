package repository_test

import (
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
	"time"
)

func TestReadUndoRestoresOnlyThisOperation(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO users(id,username,password_hash) VALUES(1,'reader','x'),(2,'other','x')`)
	must(`INSERT INTO feeds(id,url,title,owner_id) VALUES(1,'https://one','one',1)`)
	must(`INSERT INTO articles(id,feed_id,title,url) VALUES(1,1,'partial','1'),(2,1,'read','2'),(3,1,'new','3'),(4,1,'changed later','4')`)
	must(`INSERT INTO reading_progress(user_id,article_id,scroll_position,is_completed,last_read_at) VALUES(1,1,0.4,false,'2026-10-26'),(1,2,1,true,'2026-10-26')`)
	repo := repository.NewProgressRepository(db)
	result, err := repo.MarkAllReadWithUndo(1, nil, false, false, repository.MarkAllReadClipFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ArticleIDs) != 3 || result.UndoToken == "" {
		t.Fatalf("result=%+v", result)
	}
	// A later real read must survive undo, even for an article this operation changed.
	must(`UPDATE reading_progress SET last_read_at=last_read_at+interval '1 second' WHERE article_id=4 AND user_id=1`)
	if _, err := repo.UndoMarkRead(2, result.UndoToken); err == nil {
		t.Fatal("foreign user consumed undo")
	}
	ids, err := repo.UndoMarkRead(1, result.UndoToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("undone=%v", ids)
	}
	var position float64
	var completed bool
	if err := db.QueryRow(`SELECT scroll_position,is_completed FROM reading_progress WHERE article_id=1 AND user_id=1`).Scan(&position, &completed); err != nil {
		t.Fatal(err)
	}
	if position != 0.4 || completed {
		t.Fatalf("partial restored to %v %v", position, completed)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM reading_progress WHERE article_id IN (2,4) AND is_completed`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("existing/later read affected: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM reading_progress WHERE article_id=3`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("new row not removed: %d %v", count, err)
	}
	if _, err := repo.UndoMarkRead(1, result.UndoToken); err == nil {
		t.Fatal("token replay accepted")
	}
	result, err = repo.MarkReadRangeWithUndo(repository.ReadRange{Filter: repository.ArticleFilter{UserID: 1}, AnchorID: 1, Direction: "single"})
	if err != nil {
		t.Fatal(err)
	}
	must(`UPDATE reading_read_undo SET expires_at=NOW()-interval '1 second' WHERE token=$1`, result.UndoToken)
	if _, err := repo.UndoMarkRead(1, result.UndoToken); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestReadUndoPreservesConcurrentProgress(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "insert"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			must := func(q string) {
				t.Helper()
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			must(`INSERT INTO users(id,username,password_hash) VALUES(1,'reader','x')`)
			must(`INSERT INTO feeds(id,url,title,owner_id) VALUES(1,'https://one','one',1)`)
			must(`INSERT INTO articles(id,feed_id,title,url) VALUES(1,1,'concurrent','1'),(2,1,'target','2')`)
			if existing {
				must(`INSERT INTO reading_progress(user_id,article_id,scroll_position,is_completed) VALUES(1,1,0.2,false)`)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if existing {
				_, err = tx.Exec(`UPDATE reading_progress SET scroll_position=0.6,last_read_at=NOW() WHERE user_id=1 AND article_id=1`)
			} else {
				_, err = tx.Exec(`INSERT INTO reading_progress(user_id,article_id,scroll_position,is_completed) VALUES(1,1,0.6,false)`)
			}
			if err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				result repository.ReadMutationResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := repository.NewProgressRepository(db).MarkAllReadWithUndo(1, nil, false, false, repository.MarkAllReadClipFilter{})
				done <- outcome{result, err}
			}()
			// Observe the real transaction lock: mark-read has taken its statement
			// snapshot and is blocked on the other device's uncommitted progress.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'WITH scoped AS MATERIALIZED%')`).Scan(&blocked)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mark-read did not block")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			result := <-done
			if result.err != nil {
				t.Fatal(result.err)
			}
			if _, err = repository.NewProgressRepository(db).UndoMarkRead(1, result.result.UndoToken); err != nil {
				t.Fatal(err)
			}
			var position float64
			var completed bool
			if err = db.QueryRow(`SELECT scroll_position,is_completed FROM reading_progress WHERE user_id=1 AND article_id=1`).Scan(&position, &completed); err != nil {
				t.Fatal(err)
			}
			if position != 0.6 || completed {
				t.Fatalf("concurrent progress lost: %v %v", position, completed)
			}
		})
	}
}
