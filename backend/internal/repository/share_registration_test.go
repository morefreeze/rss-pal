package repository

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/registrationpolicy"
	"testing"
	"time"
)

func TestShareRegistrationWaitsForRevocation(t *testing.T) {
	f := newShareRepoFixture(t)
	const id = "0123456789abcdef0123456789abcdef"
	if _, err := f.db.Exec(`INSERT INTO article_shares(public_id,article_id,created_by,snapshot,short_code) VALUES($1,$2,$3,'{}','o6XKeCZTDZ22')`, id, f.article.ID, f.userA); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE article_shares SET revoked_at=NOW() WHERE public_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := NewUserRepository(f.db).RegisterFromShare(ctx, "late-reader", "test-password", ShareRegistrationSource{ShortCode: "o6XKeCZTDZ22"}, registrationpolicy.Parse("all", ""))
		result <- err
	}()
	// An in-flight revocation must not allow an account against the old row.
	select {
	case err := <-result:
		t.Fatalf("registration did not wait for revocation: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrInvalidRegistrationShare) {
		t.Fatalf("got %v", err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='late-reader'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("revoked invitation created account n=%d err=%v", n, err)
	}
}

func TestShareRegistrationRejectsExpiryWhileWaitingForLock(t *testing.T) {
	f := newShareRepoFixture(t)
	const id = "0123456789abcdef0123456789abcdef"
	if _, err := f.db.Exec(`INSERT INTO article_shares(public_id,article_id,created_by,snapshot,short_code,expires_at) VALUES($1,$2,$3,'{}','o6XKeCZTDZ22',clock_timestamp()+INTERVAL '300 milliseconds')`, id, f.article.ID, f.userA); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT public_id FROM article_shares WHERE public_id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := NewUserRepository(f.db).RegisterFromShare(ctx, "expired-reader", "test-password", ShareRegistrationSource{ShortCode: "o6XKeCZTDZ22"}, registrationpolicy.Parse("all", ""))
		result <- err
	}()
	// Database time advances across expiry while the share row is locked.
	if _, err = tx.Exec(`SELECT pg_sleep(0.4)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrInvalidRegistrationShare) {
		t.Fatalf("got %v", err)
	}
}
