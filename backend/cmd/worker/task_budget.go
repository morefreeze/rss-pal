package main

import (
	"context"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

var workerBudgets *taskbudget.Store
var workerPolicies taskbudget.Policies

// Production main always configures the ledger before starting work. Nil is
// only used by isolated tests which don't execute production main.
func admitBackground(ctx context.Context, bucket string, eligible func(context.Context) (bool, error)) (func(), error) {
	if workerBudgets == nil {
		if eligible != nil {
			due, err := eligible(ctx)
			if err != nil {
				return nil, err
			}
			if !due {
				return nil, taskbudget.ErrNoLongerEligible
			}
		}
		return func() {}, nil
	}
	return workerBudgets.AcquireWaiting(ctx, taskbudget.Owner(ctx), bucket, 1, workerPolicies[bucket], eligible)
}
