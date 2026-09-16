package opsmonitor

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/lib/pq"
	"os"
	"testing"
)

func TestMigrationReplayWithNonSuperuserOwner(t *testing.T) {
	db, schema, done := testdb.NewWithSchema(t)
	defer done()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	if _, err := db.Exec(`GRANT CREATE ON SCHEMA ` + pq.QuoteIdentifier(schema) + ` TO rsspal_app; ALTER TABLE operations_events OWNER TO rsspal_app; ALTER TABLE operations_collection OWNER TO rsspal_app`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/045_operations_monitoring.sql")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := app.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), string(migration)); err != nil {
		t.Fatal(err)
	}
	var leaked bool
	if err = conn.QueryRowContext(context.Background(), `SELECT app_rls_bypass()`).Scan(&leaked); err != nil || leaked {
		t.Fatalf("migration left bypass enabled=%v, err=%v", leaked, err)
	}
}
