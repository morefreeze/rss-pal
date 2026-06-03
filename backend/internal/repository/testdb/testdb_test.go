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
