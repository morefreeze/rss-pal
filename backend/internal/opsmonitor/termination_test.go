package opsmonitor

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestRecorderTerminationProcess(t *testing.T) {
	if dsn := os.Getenv("OPS_MONITOR_SIGNAL_TEST_DSN"); dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			panic(err)
		}
		recorder := NewRecorder(db)
		defer recorder.Close()
		stop := recorder.InstallTerminationHandler(func(ctx context.Context) {
			// Simulate an accepted HTTP request completing during quiescence.
			recorder.Record(Event{Kind: "registration", Reason: "success", Count: 2})
		})
		defer stop()
		recorder.Record(Event{Kind: "captcha", Reason: "unavailable", Count: 7})
		fmt.Println("READY")
		select {}
	}
	db, done := testdb.New(t)
	defer done()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecorderTerminationProcess$")
	cmd.Env = append(os.Environ(), "OPS_MONITOR_SIGNAL_TEST_DSN="+u.String())
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() || scanner.Text() != "READY" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child did not start")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("SIGTERM shutdown: %v", err)
	}
	var n int
	if err = db.QueryRow(`SELECT COALESCE(sum(count),0) FROM operations_events`).Scan(&n); err != nil || n != 9 {
		t.Fatalf("buffered+in-flight events=%d err=%v", n, err)
	}
}
func TestRecorderCloseConcurrentAndRejectsAfterClose(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	r := NewRecorder(db)
	finished := make(chan struct{})
	go func() { r.Close(); close(finished) }()
	r.Close()
	<-finished
	r.Record(Event{Kind: "captcha", Reason: "rejected"})
	if len(r.input) != 0 {
		t.Fatal("closed recorder enqueued event")
	}
}
