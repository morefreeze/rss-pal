package opsmonitor

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
	"time"
)

func TestExploreEstimateSchedule(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	for _, tc := range []struct {
		name, now, want string
		waiting         int
		consumed        bool
	}{
		{"overnight", "2026-10-09T23:05:00", "2026-10-10T07:40:00", 500, false},
		{"shared capacity", "2026-10-09T09:00:00", "2026-10-09T13:40:00", 501, false},
		{"current window unused", "2026-10-09T10:35:00", "2026-10-09T10:45:00", 500, false},
		{"current window consumed", "2026-10-09T10:35:00", "2026-10-09T13:40:00", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.ParseInLocation("2006-01-02T15:04:05", tc.now, loc)
			got := estimateExplore(now, tc.waiting, 500, 600, tc.consumed)
			if got.Format("2006-01-02T15:04:05") != tc.want {
				t.Fatalf("got %v want %s", got, tc.want)
			}
		})
	}
}

func TestExploreEstimateHistoryAndStates(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	svc := NewService(db, DefaultConfig(), nil)
	now := time.Date(2026, 10, 9, 2, 35, 0, 0, time.UTC)
	r := Response{GeneratedAt: now, Queues: []Queue{{Name: "explore_fetch_queue", Waiting: 300}, {Name: "explore_related_tasks", Waiting: 201}}}
	check := func(status string) {
		t.Helper()
		if err := svc.exploreEstimate(context.Background(), &r); err != nil {
			t.Fatal(err)
		}
		if r.ExploreEstimate.Status != status {
			t.Fatalf("%+v", r.ExploreEstimate)
		}
	}
	check("insufficient_data")
	for i := 1; i <= 3; i++ {
		start := now.Add(-time.Duration(i) * 24 * time.Hour)
		if _, err := db.Exec(`INSERT INTO explore_fetch_runs(window_at,status,claimed_count,started_at,completed_at) VALUES($3,'done',500,$1,$2)`, start, start.Add(10*time.Minute), start.Add(8*time.Hour-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	check("estimated")
	if r.ExploreEstimate.Batches != 2 || r.ExploreEstimate.SampleCount != 3 || r.ExploreEstimate.CompletionAt.UTC() != time.Date(2026, 10, 9, 5, 45, 0, 0, time.UTC) {
		t.Fatalf("%+v", r.ExploreEstimate)
	}
	// The current Shanghai window is stored as wall time, not as UTC.
	if _, err := db.Exec(`INSERT INTO explore_fetch_runs(window_at,status,claimed_count) VALUES('2026-10-09 10:30:00','done',0)`); err != nil {
		t.Fatal(err)
	}
	check("estimated")
	if r.ExploreEstimate.CompletionAt.UTC() != time.Date(2026, 10, 9, 8, 45, 0, 0, time.UTC) {
		t.Fatalf("consumed window: %+v", r.ExploreEstimate)
	}
	n := 1
	r.Queues[0].Running = &n
	check("busy")
	r.Queues[0].Running = nil
	r.Queues[0].Expired = &n
	check("busy")
	svc.cfg.ExploreBatchLimit = 60
	r.Queues[0].Expired = nil
	check("estimated")
	if r.ExploreEstimate.Batches != 9 {
		t.Fatalf("non-default limit: %+v", r.ExploreEstimate)
	}
	r.Queues = nil
	check("empty")
	r.Queues = []Queue{{Name: "explore_fetch_queue", Waiting: 1}}
	r.GeneratedAt = now.Add(10 * 24 * time.Hour)
	check("insufficient_data")
}
