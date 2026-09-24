package taskbudget_test

import (
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"testing"
)

func TestSubscriptionPolicyIndependent(t *testing.T) {
	policies, err := taskbudget.LoadPolicies()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := policies["subscription_fetch"]
	if !ok || p.Daily != 500 || p.AdminDaily != 2000 || p.GlobalDaily != 5000 || p.Concurrent != 2 || p.GlobalConcurrent != 5 {
		t.Fatalf("subscription policy = %+v", p)
	}
	t.Setenv("TASK_SUBSCRIPTION_FETCH_ADMIN_DAILY", "2400")
	t.Setenv("TASK_SUBSCRIPTION_FETCH_DAILY", "600")
	t.Setenv("TASK_SUBSCRIPTION_FETCH_GLOBAL_DAILY", "7000")
	t.Setenv("TASK_SUBSCRIPTION_FETCH_CONCURRENT", "3")
	t.Setenv("TASK_SUBSCRIPTION_FETCH_GLOBAL_CONCURRENT", "7")
	updated, err := taskbudget.LoadPolicies()
	if err != nil {
		t.Fatal(err)
	}
	got := updated["subscription_fetch"]
	if got.AdminDaily != 2400 || got.Daily != 600 || got.GlobalDaily != 7000 || got.Concurrent != 3 || got.GlobalConcurrent != 7 {
		t.Fatalf("overrides ignored: %+v", got)
	}
	if updated["background_fetch"] != policies["background_fetch"] {
		t.Fatal("subscription overrides changed backfill")
	}
	for _, name := range []string{"ADMIN_DAILY", "DAILY", "GLOBAL_DAILY", "CONCURRENT", "GLOBAL_CONCURRENT"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TASK_SUBSCRIPTION_FETCH_"+name, "0")
			if _, err := taskbudget.LoadPolicies(); err == nil {
				t.Fatal("accepted zero quota")
			}
		})
	}
}
