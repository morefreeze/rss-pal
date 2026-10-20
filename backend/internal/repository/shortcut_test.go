package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestShortcutConnectionLifecycle(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var a, b int
	db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('shortcut-a','x') RETURNING id`).Scan(&a)
	db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('shortcut-b','x') RETURNING id`).Scan(&b)
	r := NewShortcutRepository(db)
	code, _, err := r.Pair(a)
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := r.Pair(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Connect(code); !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("old code accepted: %v", err)
	}
	token, err := r.Connect(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Connect(newer); !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("replay accepted: %v", err)
	}
	tx, _ := db.Begin()
	defer tx.Rollback()
	uid, err := ResolveShortcutOwner(tx, token)
	if err != nil || uid != a {
		t.Fatalf("owner=%d err=%v", uid, err)
	}
	tx.Rollback()
	if n, err := r.Count(b); err != nil || n != 0 {
		t.Fatalf("other account count=%d err=%v", n, err)
	}
	if err := r.Revoke(b); err != nil {
		t.Fatal(err)
	}
	tx, _ = db.Begin()
	uid, err = ResolveShortcutOwner(tx, token)
	tx.Rollback()
	if err != nil || uid != a {
		t.Fatal("other account revoked credential")
	}
	pending, _, _ := r.Pair(a)
	if err := r.Revoke(a); err != nil {
		t.Fatal(err)
	}
	tx, _ = db.Begin()
	_, err = ResolveShortcutOwner(tx, token)
	tx.Rollback()
	if !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("revoked credential accepted: %v", err)
	}
	if _, err := r.Connect(pending); !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("revoked pending accepted: %v", err)
	}
}

func TestShortcutExpiredAndConcurrentExchange(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var uid int
	db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('shortcut','x') RETURNING id`).Scan(&uid)
	r := NewShortcutRepository(db)
	code, expires, err := r.Pair(uid)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(expires) < 4*time.Minute || time.Until(expires) > 5*time.Minute {
		t.Fatalf("expiry=%s", expires)
	}
	if _, err = db.Exec(`UPDATE shortcut_pairings SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Connect(code); !errors.Is(err, ErrShortcutConnectionInvalid) {
		t.Fatalf("expired accepted: %v", err)
	}
	code, _, _ = r.Pair(uid)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := r.Connect(code); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrShortcutConnectionInvalid) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful exchanges=%d", success)
	}
	var stored string
	if err := db.QueryRow(`SELECT token_hash FROM shortcut_connections`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 64 {
		t.Fatalf("invalid stored hash length=%d", len(stored))
	}
	for _, bad := range []string{"", code, "not-a-token"} {
		tx, _ := db.Begin()
		_, err := ResolveShortcutOwner(tx, bad)
		tx.Rollback()
		if !errors.Is(err, ErrShortcutConnectionInvalid) {
			t.Fatalf("bad token accepted: %v", err)
		}
	}
}
