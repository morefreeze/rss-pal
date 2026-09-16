package opsmonitor

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// One bounded consumer per process. Record never waits for database or starts goroutines.
type Recorder struct {
	mu      sync.RWMutex
	closed  bool
	db      *sql.DB
	input   chan Event
	dropped atomic.Int64
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewRecorder(db *sql.DB) *Recorder {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Recorder{db: db, input: make(chan Event, 2048), cancel: cancel, done: make(chan struct{})}
	go r.run(ctx)
	return r
}
func (r *Recorder) Close() {
	if r != nil && r.cancel != nil {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		r.cancel()
		<-r.done
	}
}
func (r *Recorder) Record(e Event) {
	if r == nil || !validEvent(e) {
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return
	}
	e.At = time.Now().UTC()
	if e.Count < 1 {
		e.Count = 1
	}
	select {
	case r.input <- e:
	default:
		r.dropped.Add(int64(e.Count))
	}
}
func validEvent(e Event) bool {
	if e.UserID < 0 || !validTask(e.TaskType) {
		return false
	}
	switch e.Kind {
	case "registration":
		return e.Reason == "success" || e.Reason == "failed"
	case "captcha":
		return e.Reason == "success" || e.Reason == "rejected" || e.Reason == "unavailable"
	case "limit":
		switch e.Reason {
		case "user_daily", "global_daily", "user_concurrency", "global_concurrency", "network", "account", "global", "unavailable":
			return true
		}
	}
	return false
}
func (r *Recorder) run(ctx context.Context) {
	defer close(r.done)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	cleanup := time.NewTicker(time.Minute)
	defer cleanup.Stop()
	r.cleanup()
	for {
		if ctx.Err() != nil {
			r.flush()
			return
		}
		select {
		case <-ctx.Done():
			r.flush()
			return
		case <-tick.C:
			r.flush()
		case <-cleanup.C:
			r.cleanup()
		}
	}
}
func (r *Recorder) flush() { // At most 128 aggregated rows per minute per process, even under attack.
	// Dropped samples are persisted separately so incomplete data is never healthy.
	batch := map[Event]int{}
	retries := map[Event]*time.Time{}
	for i := 0; i < 2048; i++ {
		select {
		case e := <-r.input:
			e.At = e.At.Truncate(time.Minute)
			n := e.Count
			e.Count = 0
			retry := e.RetryAt
			e.RetryAt = nil
			if _, exists := batch[e]; !exists && len(batch) >= 128 {
				r.dropped.Add(int64(n))
				continue
			}
			batch[e] += n
			if retry != nil {
				v := retry.UTC().Truncate(time.Second)
				retries[e] = &v
			}
		default:
			i = 2048
		}
	}
	if len(batch) == 0 && r.dropped.Load() == 0 {
		return
	}
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.fail(batch)
		return
	}
	defer tx.Rollback()
	for e, n := range batch {
		if _, err = tx.ExecContext(ctx, `INSERT INTO operations_events(at,kind,reason,task_type,user_id,count,retry_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, e.At, e.Kind, e.Reason, e.TaskType, e.UserID, n, retries[e]); err != nil {
			r.fail(batch)
			return
		}
	}
	d := r.dropped.Swap(0)
	if _, err = tx.ExecContext(ctx, `UPDATE operations_collection SET dropped=dropped+$1 WHERE id`, d); err != nil {
		r.dropped.Add(d)
		r.fail(batch)
		return
	}
	if err = tx.Commit(); err != nil {
		r.dropped.Add(d)
		r.fail(batch)
	}
}
func (r *Recorder) fail(batch map[Event]int) {
	for _, n := range batch {
		r.dropped.Add(int64(n))
	}
	log.Print("operations monitoring collection unavailable")
}
func (r *Recorder) cleanup() {
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	_, err := r.db.ExecContext(ctx, `DELETE FROM operations_events WHERE id IN (SELECT id FROM operations_events WHERE at<now()-interval '30 days' ORDER BY at LIMIT 10000)`)
	if err != nil {
		log.Print("operations monitoring retention unavailable")
	}
}
