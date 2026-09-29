package taskbudget_test

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"strings"
	"testing"
)

func TestAutomaticAndManualAIPoolsAreIndependent(t *testing.T) {
	policies, err := taskbudget.LoadPolicies()
	if err != nil {
		t.Fatal(err)
	}
	if policies["ai_auto"].Daily != 100 || policies["ai_manual"].Daily != 20 {
		t.Fatalf("wrong pools: auto=%+v manual=%+v", policies["ai_auto"], policies["ai_manual"])
	}
	db, done := testdb.New(t)
	defer done()
	s := taskbudget.New(db)
	ctx := context.Background()
	// Historic shared consumption must not spend either new pool.
	if _, err = db.Exec(`INSERT INTO task_budget_daily VALUES(1,'ai',(clock_timestamp() AT TIME ZONE 'UTC')::date,50)`); err != nil {
		t.Fatal(err)
	}
	auto, e := s.Acquire(ctx, 1, "ai_auto", 100, policies["ai_auto"])
	if e != nil {
		t.Fatal(e)
	}
	defer auto()
	if _, e = s.Acquire(ctx, 1, "ai_auto", 1, policies["ai_auto"]); !errors.Is(e, taskbudget.ErrExceeded) {
		t.Fatalf("automatic exceeded: %v", e)
	}
	manual, e := s.Acquire(ctx, 1, "ai_manual", 20, policies["ai_manual"])
	if e != nil {
		t.Fatalf("automatic exhausted manual: %v", e)
	}
	defer manual()
	if _, e = s.Acquire(ctx, 1, "ai_manual", 1, policies["ai_manual"]); !errors.Is(e, taskbudget.ErrExceeded) {
		t.Fatalf("manual exceeded: %v", e)
	}
	// Saturate automatic concurrency for a different owner; manual stays available.
	a, e := s.Acquire(ctx, 2, "ai_auto", 1, policies["ai_auto"])
	if e != nil {
		t.Fatal(e)
	}
	defer a()
	b, e := s.Acquire(ctx, 2, "ai_auto", 1, policies["ai_auto"])
	if e != nil {
		t.Fatal(e)
	}
	defer b()
	if _, e = s.Acquire(ctx, 2, "ai_auto", 1, policies["ai_auto"]); !errors.Is(e, taskbudget.ErrExceeded) {
		t.Fatalf("concurrency: %v", e)
	}
	m, e := s.Acquire(ctx, 2, "ai_manual", 1, policies["ai_manual"])
	if e != nil {
		t.Fatalf("auto blocked manual concurrency: %v", e)
	}
	m()
}

func TestAIPoolErrorNamesItsPool(t *testing.T) {
	for bucket, label := range map[string]string{"ai_auto": "自动", "ai_manual": "手动"} {
		e := &taskbudget.LimitError{Reason: "user_daily", Bucket: bucket, Used: 20, Limit: 20}
		if !strings.Contains(e.Error(), label) || strings.Contains(e.Error(), "共用") {
			t.Fatalf("wrong pool message: %v", e)
		}
	}
}
