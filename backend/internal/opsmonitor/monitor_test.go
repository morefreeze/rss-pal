package opsmonitor

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"os"
	"testing"
	"time"
)

func TestSnapshotWindowsAndNoDoubleCount(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	cfg := DefaultConfig()
	s := NewService(db, cfg, taskbudget.Policies{"ai": {GlobalDaily: 10}})
	r, e := s.Snapshot(context.Background(), 24, 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "no_data" || r.Cost.EstimatedToday != nil {
		t.Fatalf("empty %+v", r)
	}
	_, e = db.Exec(`INSERT INTO operations_events(at,kind,reason,task_type,user_id,count) VALUES(now(),'registration','success','register',1,2),(now(),'captcha','unavailable','register',0,3),(now()-interval '2 hours','captcha','rejected','register',0,5); INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES(0,'ai',(now() at time zone 'UTC')::date,8),(1,'ai',(now() at time zone 'UTC')::date,8)`)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.Snapshot(context.Background(), 1, 0, 1)
	if e != nil {
		t.Fatal(e)
	}
	if r.Registration.Success != 2 || r.Captcha.Rejected != 0 || r.Captcha.Unavailable != 3 || len(r.RecentEvents) != 1 || r.NextBeforeID == nil {
		t.Fatalf("snapshot %+v", r)
	}
	if len(r.Cost.ByUser) != 1 || r.Cost.ByTask[0].TodayAttempts != 8 || len(r.Alerts) != 2 {
		t.Fatalf("cost/alerts %+v", r)
	}
}
func TestConfigRejectsInvalidRates(t *testing.T) {
	t.Setenv("OPS_MONITOR_UNIT_PRICES", `{"ai":-1}`)
	if _, e := LoadConfig(); e == nil {
		t.Fatal("negative rate accepted")
	}
}
func TestRecorderControlledBounded(t *testing.T) {
	r := &Recorder{input: make(chan Event, 1)}
	r.Record(Event{Kind: "secret", Reason: "secret"})
	if len(r.input) != 0 {
		t.Fatal("uncontrolled event accepted")
	}
	r.Record(Event{Kind: "captcha", Reason: "rejected"})
	r.Record(Event{Kind: "captcha", Reason: "rejected"})
	if r.dropped.Load() != 1 {
		t.Fatal("overflow not counted")
	}
	_ = time.Second
}

func TestRecorderBatchPreservesCountsAndRetry(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	r := &Recorder{db: db, input: make(chan Event, 2048)}
	retry := time.Now().UTC().Add(time.Hour)
	r.Record(Event{Kind: "limit", Reason: "global_daily", TaskType: "ai", Count: 7, RetryAt: &retry})
	r.flush()
	var n int
	var at time.Time
	if e := db.QueryRow(`SELECT count,retry_at FROM operations_events`).Scan(&n, &at); e != nil {
		t.Fatal(e)
	}
	if n != 7 || at.Unix() != retry.Unix() {
		t.Fatalf("count %d retry %v", n, at)
	}
}
func TestQueuesAndSummaryEligibility(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	_, e := db.Exec(`INSERT INTO feeds(url,title,status,is_active) VALUES('https://monitor-active','active','active',true),('https://monitor-paused','paused','paused',false); INSERT INTO articles(feed_id,title,url,content,fetched_at) SELECT id,'article',url||'/article',repeat('x',101),now()-interval '31 minutes' FROM feeds;`)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewService(db, DefaultConfig(), nil).Snapshot(context.Background(), 24, 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	articles, err := repository.NewArticleRepository(db).GetArticlesWithoutSummary(10)
	if err != nil || len(articles) != 1 {
		t.Fatalf("selector len=%d err=%v", len(articles), err)
	}
	if r.Queues[0].Waiting != 1 || r.Queues[0].Running != nil || len(r.Alerts) != 1 {
		t.Fatalf("queues %+v alerts %+v", r.Queues, r.Alerts)
	}
}
func TestConfiguredCostAndUTCDate(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	_, e := db.Exec(`INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES(0,'ai',(now() at time zone 'UTC')::date,4),(0,'ai',(now() at time zone 'UTC')::date-1,6),(10,'ai',(now() at time zone 'UTC')::date,4)`)
	if e != nil {
		t.Fatal(e)
	}
	c := DefaultConfig()
	c.UnitPrices["ai"] = 0.5
	b := 2.
	c.DailyCostBudget = &b
	r, e := NewService(db, c, taskbudget.Policies{"ai": {GlobalDaily: 10}}).Snapshot(context.Background(), 168, 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	if r.Cost.EstimatedToday == nil || *r.Cost.EstimatedToday != 2 || r.Cost.ByTask[0].Attempts != 10 || len(r.Cost.ByUser) != 1 {
		t.Fatalf("cost %+v", r.Cost)
	}
	if len(r.Alerts) != 1 || r.Alerts[0].Code != "estimated_cost" {
		t.Fatalf("alerts %+v", r.Alerts)
	}
}

func TestMigrationIdempotentAndRetention(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	b, e := os.ReadFile("../../migrations/045_operations_monitoring.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(b)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO operations_events(at,kind,reason,count) VALUES(now()-interval '31 days','captcha','rejected',1),(now(),'captcha','rejected',1)`); e != nil {
		t.Fatal(e)
	}
	r := &Recorder{db: db}
	r.cleanup()
	var n int
	if e = db.QueryRow(`SELECT count(*) FROM operations_events`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("retention count %d err %v", n, e)
	}
}

func TestAlertBoundaryAndPartialPrices(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	_, e := db.Exec(`INSERT INTO operations_events(at,kind,reason,count) VALUES(now(),'captcha','unavailable',3),(now(),'captcha','success',12),(now(),'limit','global',20); INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES(0,'ai',(now() at time zone 'UTC')::date,4),(0,'interactive',(now() at time zone 'UTC')::date,4)`)
	if e != nil {
		t.Fatal(e)
	}
	cfg := DefaultConfig()
	cfg.UnitPrices["ai"] = 1
	r, e := NewService(db, cfg, nil).Snapshot(context.Background(), 1, 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Alerts) != 1 || r.Alerts[0].Code != "limit_denied" {
		t.Fatalf("20 percent is not over threshold: %+v", r.Alerts)
	}
	if r.Cost.EstimateStatus != "partial" || r.Cost.EstimatedToday == nil || *r.Cost.EstimatedToday != 4 {
		t.Fatalf("partial %+v", r.Cost)
	}
	if _, e = db.Exec(`UPDATE operations_events SET count=11 WHERE reason='success'`); e != nil {
		t.Fatal(e)
	}
	r, e = NewService(db, cfg, nil).Snapshot(context.Background(), 1, 0, 50)
	if e != nil || len(r.Alerts) != 2 {
		t.Fatalf("over threshold alerts %+v err %v", r.Alerts, e)
	}
}
func TestRecorderFailureIsBoundedAndReported(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	r := &Recorder{db: db, input: make(chan Event, 2)}
	if _, e := db.Exec(`DROP TABLE operations_events`); e != nil {
		t.Fatal(e)
	}
	r.Record(Event{Kind: "captcha", Reason: "unavailable", Count: 3})
	r.flush()
	if r.dropped.Load() != 3 || len(r.input) != 0 {
		t.Fatal("failed persistence not reported")
	}
}
