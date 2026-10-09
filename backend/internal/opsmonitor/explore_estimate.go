package opsmonitor

import (
	"context"
	"github.com/bytedance/rss-pal/internal/explore"
	"math"
	"time"
)

type ExploreEstimate struct {
	Status       string     `json:"status"`
	Waiting      int        `json:"waiting"`
	Batches      int        `json:"batches"`
	SampleCount  int        `json:"sample_count"`
	Seconds      *int64     `json:"seconds,omitempty"`
	CompletionAt *time.Time `json:"completion_at,omitempty"`
}

func (s *Service) exploreEstimate(ctx context.Context, r *Response) error {
	e := ExploreEstimate{Status: "empty"}
	busy := false
	for _, q := range r.Queues {
		if q.Name != "explore_fetch_queue" && q.Name != "explore_related_tasks" {
			continue
		}
		e.Waiting += q.Waiting
		busy = busy || (q.Running != nil && *q.Running > 0) || (q.Expired != nil && *q.Expired > 0)
	}
	if busy {
		e.Status = "busy"
		r.ExploreEstimate = e
		return nil
	}
	if e.Waiting == 0 {
		r.ExploreEstimate = e
		return nil
	}
	// These timestamps are stored without a timezone: execution times are UTC,
	// whereas window_at preserves the scheduler's Shanghai wall clock.
	var secondsPerTask, syncSeconds float64
	err := s.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(seconds)/NULLIF(sum(claimed_count),0),0),COALESCE(avg(sync_seconds),0) FROM (
 SELECT claimed_count,extract(epoch FROM completed_at-started_at) AS seconds,
 extract(epoch FROM started_at-(window_at-interval '8 hours')) AS sync_seconds
 FROM explore_fetch_runs WHERE status='done' AND claimed_count>0
 AND started_at >= window_at-interval '8 hours'
 AND started_at < window_at-interval '8 hours'+interval '30 minutes'
 AND extract(minute FROM window_at)=30
 AND completed_at>started_at AND completed_at >= $1::timestamp-interval '7 days'
 ORDER BY completed_at DESC LIMIT 12) samples`, r.GeneratedAt.UTC()).Scan(&e.SampleCount, &secondsPerTask, &syncSeconds)
	if err != nil {
		return err
	}
	limit := s.cfg.ExploreBatchLimit
	if e.SampleCount < 3 || secondsPerTask <= 0 || limit <= 0 {
		e.Status = "insufficient_data"
		r.ExploreEstimate = e
		return nil
	}
	window := explore.ExploreScheduleAt(r.GeneratedAt).NextProviderSyncAt
	var consumed bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM explore_fetch_runs WHERE window_at=$1::timestamp)`, window.Format("2006-01-02 15:04:05")).Scan(&consumed); err != nil {
		return err
	}
	e.Status = "estimated"
	e.Batches = (e.Waiting + limit - 1) / limit
	// A partial final batch receives a full-batch time allowance: parallel work
	// and slow outliers do not scale linearly down with the remaining count.
	completion := estimateExplore(r.GeneratedAt, e.Waiting, limit, syncSeconds+secondsPerTask*float64(limit), consumed)
	seconds := int64(math.Ceil(completion.Sub(r.GeneratedAt).Seconds()))
	e.Seconds = &seconds
	e.CompletionAt = &completion
	r.ExploreEstimate = e
	return nil
}

func estimateExplore(now time.Time, waiting, limit int, batchSeconds float64, consumed bool) time.Time {
	schedule := explore.ExploreScheduleAt(now)
	start := schedule.NextProviderSyncAt
	if consumed {
		start = explore.ExploreScheduleAt(schedule.NextSlotAt).NextProviderSyncAt
	}
	if start.Before(now) {
		start = now.In(start.Location())
	}
	batches := (waiting + limit - 1) / limit
	duration := time.Duration(math.Ceil(batchSeconds)) * time.Second
	for i := 1; i < batches; i++ {
		// Skip the current window and any windows missed by a slow batch.
		end := start.Add(duration)
		cursor := start.Add(30 * time.Minute)
		if end.After(cursor) {
			cursor = end
		}
		start = explore.ExploreScheduleAt(cursor).NextProviderSyncAt
		if start.Before(end) {
			start = end
		}
	}
	return start.Add(duration)
}
