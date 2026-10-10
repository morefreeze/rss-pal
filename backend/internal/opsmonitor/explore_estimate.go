package opsmonitor

import (
	"context"
	"math"
	"time"
)

type ExploreEstimate struct {
	Status       string     `json:"status"`
	Waiting      int        `json:"waiting"`
	Running      int        `json:"running"`
	Remaining    int        `json:"remaining"`
	Batches      int        `json:"batches"`
	SampleCount  int        `json:"sample_count"`
	Seconds      *int64     `json:"seconds,omitempty"`
	CompletionAt *time.Time `json:"completion_at,omitempty"`
}

func (s *Service) exploreEstimate(ctx context.Context, r *Response) error {
	e := ExploreEstimate{Status: "empty"}
	expired := false
	for _, q := range r.Queues {
		if q.Name != "explore_fetch_queue" && q.Name != "explore_related_tasks" {
			continue
		}
		e.Waiting += q.Waiting
		if q.Running != nil {
			e.Running += *q.Running
		}
		expired = expired || (q.Expired != nil && *q.Expired > 0)
	}
	e.Remaining = e.Waiting + e.Running
	defer func() { r.ExploreEstimate = e }()
	if expired || r.ExploreHealth.Status == "stalled" || r.ExploreHealth.Status == "expired" {
		e.Status = "unavailable"
		return nil
	}
	if e.Remaining == 0 {
		return nil
	}
	var secondsPerTask float64
	err := s.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(seconds)/NULLIF(sum(claimed_count),0),0) FROM (SELECT claimed_count,extract(epoch FROM completed_at-started_at) AS seconds FROM explore_fetch_runs WHERE status='done' AND claimed_count>0 AND completed_at>started_at AND completed_at >= $1::timestamp-interval '7 days' AND completed_at <= $1::timestamp ORDER BY completed_at DESC LIMIT 12) samples`, r.GeneratedAt.UTC()).Scan(&e.SampleCount, &secondsPerTask)
	if err != nil {
		return err
	}
	limit := s.cfg.ExploreBatchLimit
	if limit <= 0 || secondsPerTask <= 0 || e.SampleCount < 3 {
		e.Status = "insufficient_data"
		return nil
	}
	e.Status = "estimated"
	e.Batches = (e.Waiting + limit - 1) / limit
	if e.Running > 0 {
		e.Batches++
	}
	pauses := (e.Waiting + limit - 1) / limit
	seconds := int64(math.Ceil(float64(e.Remaining)*secondsPerTask)) + int64(pauses)*120
	completion := r.GeneratedAt.Add(time.Duration(seconds) * time.Second)
	e.Seconds = &seconds
	e.CompletionAt = &completion
	return nil
}
