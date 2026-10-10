package opsmonitor

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"testing"
	"time"
)

func TestExploreHealthEligibilityAndSharedProgress(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	now := time.Now().UTC()
	ctx := context.Background()
	svc := NewService(db, DefaultConfig(), nil)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO recommended_feeds(id,url,normalized_url,title,category,language,feed_type) VALUES(900,'https://health/feed','https://health/feed','health','tech','en','rss'); INSERT INTO explore_fetch_queue(source_id,task_type,created_at,not_before) VALUES(900,'validate_source',now()-interval '30 days',now()-interval '1 minute')`)
	snapshot := func(status string) Response {
		t.Helper()
		r := Response{GeneratedAt: now}
		if err := svc.queues(ctx, &r); err != nil {
			t.Fatal(err)
		}
		if r.ExploreHealth.Status != status {
			t.Fatalf("want %s got %+v", status, r)
		}
		return r
	}
	r := snapshot("healthy")
	if r.Queues[1].OldestWaitSeconds < 86400 || r.Queues[1].ReadyWaitSeconds > 120 {
		t.Fatalf("ages %+v", r.Queues[1])
	}
	exec(`UPDATE explore_fetch_queue SET not_before=now()-interval '1 hour'`)
	snapshot("stalled")
	exec(`UPDATE explore_fetch_queue SET updated_at=now()`)
	snapshot("stalled") // producer touches do not count
	exec(`INSERT INTO explore_fetch_runs(window_at,status,claimed_count,started_at,completed_at) VALUES(now()-interval '1 minute','done',0,now(),now())`)
	snapshot("stalled")
	exec(`UPDATE explore_fetch_queue SET attempts=1,updated_at=now()-interval '1 minute'`)
	snapshot("healthy")
	exec(`UPDATE explore_fetch_queue SET updated_at=now()-interval '1 hour'`)
	snapshot("stalled")
	exec(`INSERT INTO explore_fetch_runs(id,window_at,status,started_at) VALUES(901,now(),'running',now()-interval '2 hours'); INSERT INTO explore_registry_providers(id,provider_key,provider_kind,endpoint) VALUES(902,'health','related_site','https://health')`)
	exec(`INSERT INTO explore_related_tasks(provider_id,canonical_seed_url,status,run_id,updated_at,lease_expires_at) VALUES(902,'https://health/related','leased',901,now()-interval '1 minute',now()+interval '1 hour')`)
	snapshot("healthy")
	exec(`UPDATE explore_related_tasks SET updated_at=now()-interval '1 hour'`)
	snapshot("stalled")
	exec(`UPDATE explore_related_tasks SET lease_expires_at=now()-interval '1 second'`)
	snapshot("expired")
	exec(`DELETE FROM explore_related_tasks; UPDATE explore_fetch_queue SET not_before=now()+interval '1 hour'`)
	r = snapshot("healthy")
	if r.Queues[1].Deferred != 1 || r.ExploreHealth.NoProgressSeconds != 0 {
		t.Fatalf("deferred %+v", r)
	}
}
func TestDailyExhaustionUsesCurrentPolicyAndDeduplicates(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO users(id,username,password_hash,is_admin) VALUES(991,'monitor-admin','unused',true); INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES(991,'subscription_fetch',(now() at time zone 'UTC')::date,500),(0,'subscription_fetch',(now() at time zone 'UTC')::date,5000); INSERT INTO operations_events(at,kind,reason,task_type,user_id,count) VALUES(now(),'limit','user_daily','subscription_fetch',991,500),(now(),'limit','user_daily','subscription_fetch',991,800),(now(),'limit','global_daily','subscription_fetch',991,200),(now(),'limit','global_daily','subscription_fetch',0,200)`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, DefaultConfig(), taskbudget.Policies{"subscription_fetch": {Daily: 500, AdminDaily: 2000, GlobalDaily: 5000}})
	r, err := svc.Snapshot(ctx, 24, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.QuotaExhaustions) != 1 || r.QuotaExhaustions[0].Reason != "global_daily" {
		t.Fatalf("quotas %+v", r.QuotaExhaustions)
	}
	for _, a := range r.Alerts {
		if a.Code == "limit_denied" || a.Code == "quota_subscription_fetch" {
			t.Fatalf("daily counted as burst %+v", a)
		}
	}
	svc.policies["subscription_fetch"] = taskbudget.Policy{Daily: 500, AdminDaily: 2000, GlobalDaily: 6000}
	r, err = svc.Snapshot(ctx, 24, 0, 50)
	if err != nil || len(r.QuotaExhaustions) != 0 {
		t.Fatalf("raised quota %+v %v", r.QuotaExhaustions, err)
	}
	if _, err = db.Exec(`UPDATE task_budget_daily SET used=2000 WHERE owner_id=991`); err != nil {
		t.Fatal(err)
	}
	r, err = svc.Snapshot(ctx, 24, 0, 50)
	if err != nil || len(r.QuotaExhaustions) != 1 || r.QuotaExhaustions[0].Limit != 2000 {
		t.Fatalf("admin %+v %v", r.QuotaExhaustions, err)
	}
}
