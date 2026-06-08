package testdb

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
)

const AppRolePlaceholderPassword = "rsspal_app_placeholder_change_me"

// NewAsApp opens a connection to the per-test schema as the rsspal_app role
// (NOSUPERUSER NOBYPASSRLS), so the caller's queries are actually subject
// to RLS policies — unlike New(), which inherits SUPERUSER bypass.
//
// schema must be the value returned by NewWithSchema. The returned cleanup
// only closes this connection; the schema itself is dropped by
// NewWithSchema's cleanup.
func NewAsApp(t *testing.T, schema string) (*sql.DB, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DB_URL: %v", err)
	}
	u.User = url.UserPassword("rsspal_app", AppRolePlaceholderPassword)
	q := u.Query()
	q.Del("options") // don't inherit app.bypass_rls=true from base DSN
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatalf("open rsspal_app: %v", err)
	}
	if err := db.Ping(); err != nil {
		if strings.Contains(err.Error(), "rsspal_app") || strings.Contains(err.Error(), "password authentication") {
			t.Skipf("rsspal_app role not available (run migration 034): %v", err)
		}
		t.Fatalf("ping rsspal_app: %v", err)
	}
	return db, func() { _ = db.Close() }
}
