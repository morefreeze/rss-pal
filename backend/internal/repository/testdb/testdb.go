package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// New returns a *sql.DB pointing at a fresh, migrated schema in the test DB.
// The schema is dropped via t.Cleanup. Concurrent tests get isolated schemas.
//
// This is a thin back-compat wrapper around NewWithSchema; new tests that
// also need the schema name (e.g. to open a second connection as rsspal_app)
// should call NewWithSchema directly.
func New(t *testing.T) (*sql.DB, func()) {
	db, _, cleanup := NewWithSchema(t)
	return db, cleanup
}

// NewWithSchema is like New but also returns the per-test schema name so
// callers can open additional connections (e.g. as a different role) that
// point at the same migrated schema.
func NewWithSchema(t *testing.T) (*sql.DB, string, func()) {
	return newWithSchema(t, "")
}

// NewThroughMigration creates a fresh schema after applying migrations through
// throughFile (inclusive). It is for upgrade-path tests that need to stage
// legacy data before executing the next real migration.
func NewThroughMigration(t *testing.T, throughFile string) (*sql.DB, func()) {
	db, _, cleanup := newWithSchema(t, throughFile)
	return db, cleanup
}

func newWithSchema(t *testing.T, throughFile string) (*sql.DB, string, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable"
	}
	adminDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Skipf("postgres not available at %s: %v", dsn, err)
	}
	setupLock, err := acquireTestSchemaBootstrapLock(adminDB)
	if err != nil {
		_ = adminDB.Close()
		t.Fatalf("lock test schema bootstrap: %v", err)
	}
	defer func() {
		if setupLock != nil {
			if err := setupLock.release(); err != nil {
				t.Errorf("release test schema bootstrap lock: %v", err)
			}
		}
	}()
	if err := ensurePGCrypto(setupLock.conn); err != nil {
		t.Fatalf("bootstrap pgcrypto: %v", err)
	}

	schema := fmt.Sprintf("test_%s", strings.ReplaceAll(t.Name(), "/", "_"))
	schema = strings.ToLower(schema)
	if len(schema) > 60 {
		schema = schema[:60]
	}

	if _, err := adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := adminDB.Exec(fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	schemaDSN := dsn
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	schemaDSN = fmt.Sprintf("%s%ssearch_path=%s,public", dsn, sep, schema)

	// Append app.bypass_rls=true to the session defaults so existing
	// repository tests (which don't SET app.user_id) continue to work
	// after migration 033 enables RLS on shared-owned tables. Tests that
	// specifically want to verify RLS scoping (Task 5.1) open their own
	// BeginTx + set_config('app.user_id', ..., true) on this pool —
	// SET LOCAL inside that tx overrides the session default.
	u, err := url.Parse(schemaDSN)
	if err != nil {
		t.Fatalf("parse schemaDSN: %v", err)
	}
	q := u.Query()
	existing := q.Get("options")
	bypass := "-c app.bypass_rls=true"
	if existing == "" {
		q.Set("options", bypass)
	} else if !strings.Contains(existing, "app.bypass_rls=") {
		q.Set("options", existing+" "+bypass)
	}
	u.RawQuery = q.Encode()
	schemaDSN = u.String()

	db, err := sql.Open("postgres", schemaDSN)
	if err != nil {
		t.Fatalf("open schema db: %v", err)
	}
	if err := runMigrationsThrough(db, throughFile); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := setupLock.release(); err != nil {
		t.Fatalf("release test schema bootstrap lock: %v", err)
	}
	setupLock = nil

	cleanup := func() {
		db.Close()
		_, _ = adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		adminDB.Close()
	}
	return db, schema, cleanup
}

const testSchemaBootstrapAdvisoryLock int64 = 0x72737370616c

type testSchemaBootstrapLock struct {
	ctx    context.Context
	conn   *sql.Conn
	locked bool
}

// acquireTestSchemaBootstrapLock serializes database-global bootstrap DDL
// across concurrently running Go package processes. The dedicated connection
// is retained because PostgreSQL advisory locks are session-scoped.
func acquireTestSchemaBootstrapLock(db *sql.DB) (*testSchemaBootstrapLock, error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open advisory-lock connection: %w", err)
	}
	for {
		var acquired bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, testSchemaBootstrapAdvisoryLock).Scan(&acquired); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("lock test schema bootstrap: %w", err)
		}
		if acquired {
			break
		}
		// A blocking pg_advisory_lock query keeps a virtual transaction ID
		// while waiting. CREATE INDEX CONCURRENTLY in another schema then waits
		// for that virtual transaction and deadlocks with the lock owner. Short
		// try-lock queries release their virtual IDs between attempts.
		time.Sleep(10 * time.Millisecond)
	}
	return &testSchemaBootstrapLock{ctx: ctx, conn: conn, locked: true}, nil
}

// WithSchemaBootstrapLock lets migration tests stage database-global partial
// states without racing another package's schema bootstrap. fn receives the
// same dedicated connection that owns the advisory lock.
func WithSchemaBootstrapLock(db *sql.DB, fn func(*sql.Conn) error) (err error) {
	lock, err := acquireTestSchemaBootstrapLock(db)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.release())
	}()
	return fn(lock.conn)
}

func (l *testSchemaBootstrapLock) release() (err error) {
	if l == nil || l.conn == nil {
		return nil
	}
	if l.locked {
		var unlocked bool
		unlockErr := l.conn.QueryRowContext(l.ctx, `SELECT pg_advisory_unlock($1)`, testSchemaBootstrapAdvisoryLock).Scan(&unlocked)
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock test schema bootstrap: %w", unlockErr))
		} else if !unlocked {
			err = errors.Join(err, errors.New("unlock test schema bootstrap: lock was not held"))
		}
		l.locked = false
	}
	if closeErr := l.conn.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close test schema bootstrap connection: %w", closeErr))
	}
	l.conn = nil
	return err
}

// ensurePGCrypto installs the database-global extension exactly once in
// public while the caller holds the test schema bootstrap advisory lock.
func ensurePGCrypto(conn *sql.Conn) error {
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `
		DO $pgcrypto$
		DECLARE
			installed_schema TEXT;
		BEGIN
			SELECT n.nspname
			  INTO installed_schema
			  FROM pg_extension e
			  JOIN pg_namespace n ON n.oid = e.extnamespace
			 WHERE e.extname = 'pgcrypto';
			IF installed_schema IS NOT NULL AND installed_schema <> 'public' THEN
				EXECUTE 'ALTER EXTENSION pgcrypto SET SCHEMA public';
			END IF;
		END
		$pgcrypto$;
		CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
	`); err != nil {
		return fmt.Errorf("install pgcrypto in public: %w", err)
	}
	return nil
}

func runMigrations(db *sql.DB) error {
	return runMigrationsThrough(db, "")
}

func runMigrationsThrough(db *sql.DB, throughFile string) error {
	dir := migrationsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		if throughFile != "" && name > throughFile {
			break
		}
		if err := ExecuteMigrationFile(db, name); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteMigrationFile applies one real migration file using the same
// statement splitting path as normal test schema setup.
func ExecuteMigrationFile(db *sql.DB, name string) error {
	b, err := os.ReadFile(filepath.Join(migrationsDir(), name))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, stmt := range splitSQL(string(b)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// splitSQL splits a SQL script into individual statements on top-level
// semicolons. It understands `--` line comments, `/* */` block comments,
// single-quoted string literals (with `”` escapes), and `$tag$` dollar-quoted
// strings. This is sufficient for the project's migration files; it is NOT a
// general-purpose SQL parser.
func splitSQL(s string) []string {
	var out []string
	var buf strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		// Line comment.
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				buf.WriteByte(s[i])
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			buf.WriteByte(s[i])
			buf.WriteByte(s[i+1])
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				buf.WriteByte(s[i])
				i++
			}
			if i+1 < len(s) {
				buf.WriteByte(s[i])
				buf.WriteByte(s[i+1])
				i += 2
			}
			continue
		}
		// Single-quoted string.
		if c == '\'' {
			buf.WriteByte(c)
			i++
			for i < len(s) {
				if s[i] == '\'' {
					buf.WriteByte(s[i])
					i++
					// Doubled quote escape.
					if i < len(s) && s[i] == '\'' {
						buf.WriteByte(s[i])
						i++
						continue
					}
					break
				}
				buf.WriteByte(s[i])
				i++
			}
			continue
		}
		// Dollar-quoted string ($tag$...$tag$).
		if c == '$' {
			if end := findDollarTag(s, i); end > i {
				tag := s[i:end]
				buf.WriteString(tag)
				i = end
				if idx := strings.Index(s[i:], tag); idx >= 0 {
					buf.WriteString(s[i : i+idx+len(tag)])
					i += idx + len(tag)
					continue
				}
				// Unterminated — write the rest and bail.
				buf.WriteString(s[i:])
				i = len(s)
				continue
			}
		}
		if c == ';' {
			out = append(out, buf.String())
			buf.Reset()
			i++
			continue
		}
		buf.WriteByte(c)
		i++
	}
	if strings.TrimSpace(buf.String()) != "" {
		out = append(out, buf.String())
	}
	return out
}

// findDollarTag returns the index just past a `$tag$` opener starting at i, or
// i if the position is not a dollar-quote opener.
func findDollarTag(s string, i int) int {
	if s[i] != '$' {
		return i
	}
	j := i + 1
	for j < len(s) {
		c := s[j]
		if c == '$' {
			return j + 1
		}
		// Tag chars: letters, digits, underscore.
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return i
		}
		j++
	}
	return i
}

func migrationsDir() string {
	wd, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(wd, "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		candidate = filepath.Join(wd, "backend", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		wd = filepath.Dir(wd)
	}
	panic("backend/migrations not found")
}
