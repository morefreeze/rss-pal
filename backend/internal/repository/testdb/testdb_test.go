package testdb

import (
	"testing"
)

func TestNewBootsAndRunsMigrations(t *testing.T) {
	db, cleanup := New(t)
	defer cleanup()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("expected users table to exist after migrations: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected empty users table, got %d rows", n)
	}
}

func TestNewAsApp_IsNotSuperuser(t *testing.T) {
	_, schema, cleanup := NewWithSchema(t)
	defer cleanup()

	appDB, appCleanup := NewAsApp(t, schema)
	defer appCleanup()

	var super, bypass bool
	if err := appDB.QueryRow(`
        SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user
    `).Scan(&super, &bypass); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if super || bypass {
		t.Fatalf("rsspal_app must be NOSUPERUSER NOBYPASSRLS; got super=%v bypass=%v", super, bypass)
	}
	var n int
	if err := appDB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
}

func TestNewAsApp_RLSEnforces(t *testing.T) {
	db, schema, cleanup := NewWithSchema(t)
	defer cleanup()

	var userA, userB int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('a', 'x') RETURNING id`).Scan(&userA); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('b', 'y') RETURNING id`).Scan(&userB); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	var feedID int
	if err := db.QueryRow(`INSERT INTO feeds (url, title) VALUES ('http://x', 'X') RETURNING id`).Scan(&feedID); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	var articleID int
	if err := db.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 't', 'http://x/1', NOW()) RETURNING id`, feedID).Scan(&articleID); err != nil {
		t.Fatalf("seed article: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO reading_progress (article_id, scroll_position, user_id) VALUES ($1, 0.5, $2), ($3, 0.3, $4)`, articleID, userA, articleID, userB); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	appDB, appCleanup := NewAsApp(t, schema)
	defer appCleanup()

	var n int
	if err := appDB.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n); err != nil {
		t.Fatalf("unset count: %v", err)
	}
	if n != 0 {
		t.Fatalf("unset: want 0, got %d (RLS NOT enforcing!)", n)
	}

	tx, err := appDB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, userA); err != nil {
		t.Fatalf("set_config: %v", err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n); err != nil {
		t.Fatalf("scoped count: %v", err)
	}
	if n != 1 {
		t.Fatalf("userA scoped: want 1, got %d", n)
	}
}
