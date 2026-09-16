package main

import (
	"context"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

var workerBudgets *taskbudget.Store
var workerPolicies taskbudget.Policies

// Production main always configures the ledger before starting work. Nil is
// only used by isolated tests which don't execute production main.
func admitBackground(ctx context.Context, bucket string) (func(), error) {
	if workerBudgets == nil {
		return func() {}, nil
	}
	return workerBudgets.Acquire(ctx, taskbudget.Owner(ctx), bucket, 1, workerPolicies[bucket])
}
