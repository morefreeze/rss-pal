package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

// TestBestEffort_PreservesOuterTxAfterFailure verifies the core contract:
// when bestEffort runs a function that fails inside an outer tx, the outer
// tx must still be usable afterwards (no "current transaction is aborted").
func TestBestEffort_PreservesOuterTxAfterFailure(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	// Seed something we can query to prove the outer tx is alive.
	if _, err := tx.Exec(`CREATE TEMP TABLE sp_probe(x int)`); err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO sp_probe VALUES (1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Build a minimal gin.Context carrying the outer tx as Querier.
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ctxkey.Tx, tx)

	// bestEffort closure intentionally executes a failing statement on the tx.
	// Without SAVEPOINT, this would poison the outer tx.
	bestEffort(c, "intentional failure", func() error {
		_, err := tx.Exec(`SELECT * FROM nonexistent_table_xyz`)
		return err
	})

	// Outer tx must still be usable.
	var got int
	if err := tx.QueryRow(`SELECT x FROM sp_probe`).Scan(&got); err != nil {
		t.Fatalf("outer tx aborted after best-effort failure: %v", err)
	}
	if got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
}

// TestBestEffort_RunsAndReleasesOnSuccess verifies the happy path: closure
// runs, savepoint is released, outer tx remains healthy.
func TestBestEffort_RunsAndReleasesOnSuccess(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TEMP TABLE sp_ok(x int)`); err != nil {
		t.Fatalf("create temp: %v", err)
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ctxkey.Tx, tx)

	bestEffort(c, "successful insert", func() error {
		_, err := tx.Exec(`INSERT INTO sp_ok VALUES (42)`)
		return err
	})

	// Outer tx still works, and the insert persisted within the tx.
	var got int
	if err := tx.QueryRow(`SELECT x FROM sp_ok`).Scan(&got); err != nil {
		t.Fatalf("read after best-effort success: %v", err)
	}
	if got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
}

// TestBestEffort_NoTxInContext verifies that when no tx is set (worker or
// pre-auth path), bestEffort still runs the closure and logs failures.
func TestBestEffort_NoTxInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	called := false
	bestEffort(c, "no-tx path", func() error {
		called = true
		return nil
	})
	if !called {
		t.Fatal("closure not invoked when no tx present")
	}

	// Also exercise the error branch — must not panic or affect anything.
	bestEffort(c, "no-tx failure path", func() error {
		return http.ErrAbortHandler
	})
}
