package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

const (
	ExploreTaskValidateSource  = "validate_source"
	ExploreTaskRefreshArticles = "refresh_articles"

	// Higher values are selected first. Age is added by ClaimRun so old work
	// eventually outranks continuously arriving higher-priority work.
	ExplorePriorityDirectProfile      = 400
	ExplorePriorityStructuredProvider = 300
	ExplorePriorityRefresh            = 200
	ExplorePriorityRelated            = 100

	exploreDispatcherAdvisoryLock int64 = 498571936210
	maxExploreClaim                     = 500
)

var ErrExploreDispatcherBusy = errors.New("explore dispatcher is already claiming work")

type ExploreQueueTask struct {
	ID             int
	SourceID       int
	TaskType       string
	Status         string
	Priority       int
	NotBefore      time.Time
	Attempts       int
	RunID          sql.NullInt64
	LeaseOwner     sql.NullString
	LeaseExpiresAt sql.NullTime
	LastError      sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    sql.NullTime
}

type ExploreFetchRun struct {
	ID           int
	WindowAt     time.Time
	Status       string
	ClaimedCount int
	StartedAt    sql.NullTime
	CompletedAt  sql.NullTime
	WorkerID     sql.NullString
}

// ExploreQueueRepository owns dispatcher-facing queue operations. ClaimRun
// deliberately uses the original *sql.DB because it is a worker operation,
// not a request-scoped transaction.
type ExploreQueueRepository struct {
	db    Querier
	rawDB *sql.DB
	now   func() time.Time
}

func NewExploreQueueRepository(db *sql.DB) *ExploreQueueRepository {
	return &ExploreQueueRepository{db: db, rawDB: db, now: time.Now}
}

func (r *ExploreQueueRepository) WithCtx(c ctxkey.CtxGetter) *ExploreQueueRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &ExploreQueueRepository{db: q, rawDB: r.rawDB, now: r.now}
		}
	}
	return r
}

// WithClock exists for deterministic queue tests. Production uses time.Now.
func (r *ExploreQueueRepository) WithClock(now func() time.Time) *ExploreQueueRepository {
	if now == nil {
		now = time.Now
	}
	return &ExploreQueueRepository{db: r.db, rawDB: r.rawDB, now: now}
}

func (r *ExploreQueueRepository) Enqueue(sourceID int, taskType string, priority int) (*ExploreQueueTask, error) {
	row := r.db.QueryRow(`
		INSERT INTO explore_fetch_queue (source_id, task_type, priority)
		VALUES ($1, $2, $3)
		ON CONFLICT (source_id, task_type) WHERE status IN ('pending', 'leased')
		DO UPDATE SET priority = GREATEST(explore_fetch_queue.priority, EXCLUDED.priority),
		              updated_at = NOW()
		RETURNING id, source_id, task_type, status, priority, not_before, attempts,
		          run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
	`, sourceID, taskType, priority)
	return scanExploreQueueTask(row)
}

// ClaimRun claims at most 500 fresh pending tasks across every process for one
// window. A window run is immutable after its first claim, including zero work.
func (r *ExploreQueueRepository) ClaimRun(windowAt time.Time, owner string, leaseExpiresAt time.Time, batchLimit int) (*ExploreFetchRun, []ExploreQueueTask, error) {
	if r.rawDB == nil {
		return nil, nil, errors.New("explore ClaimRun requires a database handle")
	}
	limit := sanitizeExploreLimit(batchLimit)
	tx, err := r.rawDB.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var locked bool
	if err := tx.QueryRow(`SELECT pg_try_advisory_xact_lock($1)`, exploreDispatcherAdvisoryLock).Scan(&locked); err != nil {
		return nil, nil, err
	}
	if !locked {
		return nil, nil, ErrExploreDispatcherBusy
	}

	run, inserted, err := insertOrLockExploreRun(tx, windowAt, owner)
	if err != nil {
		return nil, nil, err
	}
	if !inserted || run.ClaimedCount != 0 || run.Status != "running" {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return run, nil, nil
	}

	rows, err := tx.Query(`
		SELECT id, source_id, task_type, status, priority, not_before, attempts,
		       run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
		FROM explore_fetch_queue
		WHERE status = 'pending' AND run_id IS NULL AND not_before <= NOW()
		ORDER BY priority + FLOOR(EXTRACT(EPOCH FROM (NOW() - created_at)) / 3600)::INTEGER DESC,
		         priority DESC, created_at ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, nil, err
	}
	var tasks []ExploreQueueTask
	var ids []int64
	for rows.Next() {
		task, err := scanExploreQueueTask(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		tasks = append(tasks, *task)
		ids = append(ids, int64(task.ID))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	if len(ids) > 0 {
		if _, err := tx.Exec(`
			UPDATE explore_fetch_queue
			SET status = 'leased', run_id = $1, lease_owner = $2, lease_expires_at = $3, updated_at = NOW()
			WHERE id = ANY($4)
		`, run.ID, owner, leaseExpiresAt, pq.Array(ids)); err != nil {
			return nil, nil, err
		}
		for i := range tasks {
			tasks[i].Status = "leased"
			tasks[i].RunID = sql.NullInt64{Int64: int64(run.ID), Valid: true}
			tasks[i].LeaseOwner = sql.NullString{String: owner, Valid: true}
			tasks[i].LeaseExpiresAt = sql.NullTime{Time: leaseExpiresAt, Valid: true}
		}
	}
	run.ClaimedCount = len(tasks)
	if len(tasks) == 0 {
		run.Status = "done"
		if _, err := tx.Exec(`UPDATE explore_fetch_runs SET status = 'done', completed_at = NOW() WHERE id = $1`, run.ID); err != nil {
			return nil, nil, err
		}
	} else if _, err := tx.Exec(`UPDATE explore_fetch_runs SET claimed_count = $2 WHERE id = $1`, run.ID, len(tasks)); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return run, tasks, nil
}

func (r *ExploreQueueRepository) ListLeased(runID int, owner string) ([]ExploreQueueTask, error) {
	rows, err := r.db.Query(`
		SELECT id, source_id, task_type, status, priority, not_before, attempts,
		       run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
		FROM explore_fetch_queue WHERE run_id = $1 AND status = 'leased' AND lease_owner = $2 ORDER BY id
	`, runID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []ExploreQueueTask
	for rows.Next() {
		task, err := scanExploreQueueTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

func (r *ExploreQueueRepository) Complete(taskID int) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue SET status = 'done', completed_at = $2,
		lease_owner = NULL, lease_expires_at = NULL, updated_at = $2 WHERE id = $1 AND status = 'leased'
	`, taskID, r.now())
	return expectExploreTransition(result, err, taskID)
}

func (r *ExploreQueueRepository) Invalidate(taskID int, cause error) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue SET status = 'invalid', completed_at = $2, last_error = $3,
		lease_owner = NULL, lease_expires_at = NULL, updated_at = $2 WHERE id = $1 AND status = 'leased'
	`, taskID, r.now(), clipExploreError(cause))
	return expectExploreTransition(result, err, taskID)
}

// Retry makes a failed leased task eligible for a later run. The first retry
// waits one minute; each subsequent retry doubles, capped at one hour.
func (r *ExploreQueueRepository) Retry(taskID int, cause error) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue
		SET status = 'pending', attempts = attempts + 1,
		    not_before = $2 + (LEAST(3600, 60 * power(2, attempts)) * INTERVAL '1 second'),
		    run_id = NULL, lease_owner = NULL, lease_expires_at = NULL,
		    last_error = $3, updated_at = $2
		WHERE id = $1 AND status = 'leased'
	`, taskID, r.now(), clipExploreError(cause))
	return expectExploreTransition(result, err, taskID)
}

// RecoverExpired moves a timed-out lease to another worker without changing
// its run or that run's fixed claimed_count.
func (r *ExploreQueueRepository) RecoverExpired(runID int, newOwner string, newExpiry time.Time) ([]ExploreQueueTask, error) {
	rows, err := r.db.Query(`
		UPDATE explore_fetch_queue
		SET lease_owner = $2, lease_expires_at = $3, updated_at = $4
		WHERE run_id = $1 AND status = 'leased' AND lease_expires_at < $4
		RETURNING id, source_id, task_type, status, priority, not_before, attempts,
		          run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
	`, runID, newOwner, newExpiry, r.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []ExploreQueueTask
	for rows.Next() {
		task, err := scanExploreQueueTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

func insertOrLockExploreRun(tx *sql.Tx, windowAt time.Time, owner string) (*ExploreFetchRun, bool, error) {
	run := &ExploreFetchRun{}
	err := tx.QueryRow(`
		INSERT INTO explore_fetch_runs (window_at, status, started_at, worker_id)
		VALUES ($1, 'running', NOW(), $2)
		ON CONFLICT (window_at) DO NOTHING
		RETURNING id, window_at, status, claimed_count, started_at, completed_at, worker_id
	`, windowAt, owner).Scan(&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID)
	if err == nil {
		return run, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	err = tx.QueryRow(`
		SELECT id, window_at, status, claimed_count, started_at, completed_at, worker_id
		FROM explore_fetch_runs WHERE window_at = $1 FOR UPDATE
	`, windowAt).Scan(&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID)
	if err != nil {
		return nil, false, err
	}
	return run, false, nil
}

func sanitizeExploreLimit(limit int) int {
	if limit <= 0 || limit > maxExploreClaim {
		return maxExploreClaim
	}
	return limit
}

func scanExploreQueueTask(scanner interface{ Scan(...interface{}) error }) (*ExploreQueueTask, error) {
	task := &ExploreQueueTask{}
	err := scanner.Scan(&task.ID, &task.SourceID, &task.TaskType, &task.Status, &task.Priority, &task.NotBefore, &task.Attempts,
		&task.RunID, &task.LeaseOwner, &task.LeaseExpiresAt, &task.LastError, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func expectExploreTransition(result sql.Result, err error, taskID int) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("explore task %d is not leased", taskID)
	}
	return nil
}

func clipExploreError(cause error) string {
	if cause == nil {
		return ""
	}
	const max = 1000
	s := cause.Error()
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return s[:cut]
	}
	return s
}
