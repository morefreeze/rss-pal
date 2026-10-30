package repository

import (
	"errors"
	"sync"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestFirstPasswordSetupCannotOverwriteConcurrentChoice(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var owner, other int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('password-owner','!external') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('password-other','!external') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	repo := NewUserRepository(db)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, password := range []string{"first-choice", "second-choice"} {
		wg.Add(1)
		go func(password string) { defer wg.Done(); errs <- repo.ChangePassword(owner, "!external", password) }(password)
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrPasswordChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	u, err := repo.FindByID(owner)
	if err != nil {
		t.Fatal(err)
	}
	if u.NeedsPassword || (!repo.VerifyPassword(u, "first-choice") && !repo.VerifyPassword(u, "second-choice")) {
		t.Fatal("first password not persisted")
	}
	u, err = repo.FindByID(other)
	if err != nil || !u.NeedsPassword {
		t.Fatal("another user's password was modified")
	}
}
