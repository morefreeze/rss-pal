package taskbudget

import (
	"context"
	"errors"
	"time"
)

var ErrNoLongerEligible = errors.New("task no longer eligible")

// AcquireWaiting leaves concurrency contention out of rejection metrics. Each
// check still uses the shared database transaction, including across workers.
func (s *Store) AcquireWaiting(ctx context.Context, owner int, bucket string, cost int, p Policy, eligible func(context.Context) (bool, error)) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if eligible != nil {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			due, err := eligible(checkCtx)
			cancel()
			if err != nil {
				return nil, err
			}
			if !due {
				return nil, ErrNoLongerEligible
			}
		}

		release, err := s.acquire(ctx, owner, bucket, cost, p, true)
		var limit *LimitError
		if !errors.As(err, &limit) || (limit.Reason != "global_concurrency" && limit.Reason != "user_concurrency") {
			return release, err
		}

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
