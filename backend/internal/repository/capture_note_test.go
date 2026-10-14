package repository_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/gin-gonic/gin"
)

func TestCaptureNotesAppendReplaceAndIsolation(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()
	f.asUser(t, f.userA, func(tx *sql.Tx) {
		c := &gin.Context{}
		c.Set(ctxkey.Tx, tx)
		repo := repository.NewCaptureNoteRepository(f.appDB).WithCtx(c)
		for _, note := range []string{"  第一条  ", "", " \n ", "第二条", "第二条"} {
			if _, err := repo.Append(f.userA, f.articleA, note); err != nil {
				t.Fatal(err)
			}
		}
		got, err := repo.Get(f.userA, f.articleA)
		if err != nil || got != "第一条\n\n第二条" {
			t.Fatalf("note=%q err=%v", got, err)
		}
		if _, err := repo.Append(f.userA, f.articleB, "wrong article"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("other article err=%v", err)
		}
		if _, err := repo.Replace(f.userA, f.articleA, strings.Repeat("记", 10000)); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Append(f.userA, f.articleA, "overflow"); !errors.Is(err, repository.ErrCaptureNoteTooLong) {
			t.Fatalf("overflow err=%v", err)
		}
		got, err = repo.Get(f.userA, f.articleA)
		if err != nil || got != strings.Repeat("记", 10000) {
			t.Fatalf("overflow changed saved note: %v", err)
		}
		if _, err := repo.Replace(f.userA, f.articleA, ""); err != nil {
			t.Fatal(err)
		}
		got, err = repo.Get(f.userA, f.articleA)
		if err != nil || got != "" {
			t.Fatalf("clear note=%q err=%v", got, err)
		}
	})
}

func TestCaptureNotesDatabaseLimitAndForeignUser(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()
	if _, err := f.privDB.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,$3)`, f.userA, f.articleA, strings.Repeat("字", 10001)); err == nil {
		t.Fatal("database accepted overlong note")
	}
	f.asUser(t, f.userB, func(tx *sql.Tx) {
		if _, err := tx.Exec(`INSERT INTO article_capture_notes (user_id,article_id,capture_note) VALUES ($1,$2,'forged')`, f.userA, f.articleA); err == nil {
			t.Fatal("RLS accepted another user's note")
		}
	})
}
