package opsmonitor

import (
	"context"
	"database/sql"
	"math"
	"time"
)

func (s *Service) exploreHealth(ctx context.Context, r *Response) error {
	h := ExploreHealth{Status: "healthy", StallThresholdSeconds: s.cfg.QueueWaitSeconds}
	r.ExploreHealth = h
	ready, running, expired := 0, 0, 0
	readyAge := 0.
	for _, q := range r.Queues {
		if q.Name != "explore_fetch_queue" && q.Name != "explore_related_tasks" {
			continue
		}
		ready += q.Waiting
		readyAge = math.Max(readyAge, q.ReadyWaitSeconds)
		if q.Running != nil {
			running += *q.Running
		}
		if q.Expired != nil {
			expired += *q.Expired
		}
	}
	var progress, leasedSince sql.NullTime
	// Enqueue/defer touches cannot masquerade as worker progress. Both queues
	// share one worker pool, so forward movement in either keeps the pool healthy.
	err := s.db.QueryRowContext(ctx, `WITH tasks AS (SELECT run_id,status,attempts,updated_at FROM explore_fetch_queue UNION ALL SELECT run_id,status,attempts,updated_at FROM explore_related_tasks) SELECT (SELECT max(at) FROM (SELECT updated_at at FROM tasks WHERE run_id IS NOT NULL OR attempts>0 UNION ALL SELECT started_at FROM explore_fetch_runs WHERE started_at IS NOT NULL AND claimed_count>0) p), (SELECT min(updated_at) FROM tasks WHERE status='leased')`).Scan(&progress, &leasedSince)
	if err != nil {
		return err
	}
	if progress.Valid {
		h.LastProgressAt = &progress.Time
	}
	if ready+running > 0 {
		var since time.Time
		if ready > 0 {
			since = r.GeneratedAt.Add(-time.Duration(readyAge * float64(time.Second)))
		}
		if running > 0 && leasedSince.Valid && (since.IsZero() || leasedSince.Time.Before(since)) {
			since = leasedSince.Time
		}
		if progress.Valid && progress.Time.After(since) {
			since = progress.Time
		}
		if !since.IsZero() {
			h.NoProgressSeconds = math.Max(0, r.GeneratedAt.Sub(since).Seconds())
		}
		if h.NoProgressSeconds >= s.cfg.QueueWaitSeconds {
			h.Status = "stalled"
			r.Alerts = append(r.Alerts, Alert{"explore_stalled", "warning", "探索有可执行或执行中任务，但共享工作池长时间无进展", h.NoProgressSeconds, s.cfg.QueueWaitSeconds})
		}
	}
	if expired > 0 {
		h.Status = "expired"
		r.Alerts = append(r.Alerts, Alert{"explore_expired", "warning", "探索任务租约已过期，等待恢复", float64(expired), 0})
	}
	r.ExploreHealth = h
	return nil
}
