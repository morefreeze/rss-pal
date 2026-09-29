package opsmonitor

import (
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
)

func TestSubscriptionBudgetEventsAreRecorded(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	r := &Recorder{db: db, input: make(chan Event, 10)}
	r.Record(Event{Kind: "limit", Reason: "user_concurrency", TaskType: "subscription_fetch", UserID: 1, Count: 3})
	r.flush()
	var count int
	if err := db.QueryRow(`SELECT COALESCE(sum(count),0) FROM operations_events WHERE task_type='subscription_fetch'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("subscription events lost: %d", count)
	}
	t.Setenv("OPS_MONITOR_UNIT_PRICES", `{"subscription_fetch":0.01}`)
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
}
