package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

const (
	ExploreTaskValidateSource  = model.ExploreFetchTaskValidateSource
	ExploreTaskRefreshArticles = model.ExploreFetchTaskRefreshArticles

	ExplorePriorityDirectProfile      = 400
	ExplorePriorityStructuredProvider = 300
	ExplorePriorityRefresh            = 200
	ExplorePriorityRelated            = 100

	exploreDispatcherAdvisoryLock int64 = 498571936210
	maxExploreClaim                     = 500
	maxExplorePriority                  = 10000
)

var (
	ErrExploreDispatcherBusy = errors.New("explore dispatcher is already claiming work")
	ErrExploreLeaseNotHeld   = errors.New("explore lease is not held by this owner or has expired")
)

// Keep repository callers compatible while making model the sole source of
// persisted Explore shapes and enum values.
type ExploreQueueTask = model.ExploreFetchTask
type ExploreFetchRun = model.ExploreFetchRun

type ExploreQueueRepository struct {
	db    Querier
	rawDB *sql.DB
}

func NewExploreQueueRepository(db *sql.DB) *ExploreQueueRepository {
	return &ExploreQueueRepository{db: db, rawDB: db}
}

// WithQuerier binds queue mutations to an existing transaction while keeping
// the raw pool available for ClaimRun, which owns its transaction lifecycle.
func (r *ExploreQueueRepository) WithQuerier(db Querier) *ExploreQueueRepository {
	return &ExploreQueueRepository{db: db, rawDB: r.rawDB}
}

func (r *ExploreQueueRepository) WithCtx(c ctxkey.CtxGetter) *ExploreQueueRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return r.WithQuerier(q)
		}
	}
	return r
}

func (r *ExploreQueueRepository) Enqueue(sourceID int, taskType string, priority int) (*ExploreQueueTask, error) {
	return scanExploreQueueTask(r.db.QueryRow(`
		INSERT INTO explore_fetch_queue (source_id, task_type, priority)
		VALUES ($1, $2, $3)
		ON CONFLICT (source_id, task_type) WHERE status IN ('pending', 'leased')
		DO UPDATE SET priority = GREATEST(explore_fetch_queue.priority, EXCLUDED.priority), updated_at = CURRENT_TIMESTAMP
		RETURNING id, source_id, task_type, status, priority, not_before, attempts,
		          run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
	`, sourceID, taskType, sanitizeExplorePriority(priority)))
}

// ClaimRun atomically claims a globally capped fresh batch. A non-empty run
// intentionally remains running for Task8's finalization; a zero-work run is
// sealed done so that the same logical window never appends later work.
func (r *ExploreQueueRepository) ClaimRun(windowAt time.Time, owner string, leaseDuration time.Duration, batchLimit int) (*ExploreFetchRun, []ExploreQueueTask, error) {
	if r.rawDB == nil {
		return nil, nil, errors.New("explore ClaimRun requires a database handle")
	}
	seconds, err := exploreLeaseSeconds(leaseDuration)
	if err != nil {
		return nil, nil, err
	}
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
	if !inserted || run.ClaimedCount != 0 || run.Status != model.ExploreFetchRunRunning {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return run, nil, nil
	}

	rows, err := tx.Query(`
		WITH candidate AS (
			SELECT id FROM explore_fetch_queue
			WHERE status = 'pending' AND run_id IS NULL AND not_before <= CURRENT_TIMESTAMP
			ORDER BY priority::BIGINT + FLOOR(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - created_at)) / 3600)::BIGINT DESC,
			         priority DESC, created_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE explore_fetch_queue q
			SET status = 'leased', run_id = $2, lease_owner = $3,
			    lease_expires_at = CURRENT_TIMESTAMP + make_interval(secs => $4), updated_at = CURRENT_TIMESTAMP
			FROM candidate c WHERE q.id = c.id
			RETURNING q.id, q.source_id, q.task_type, q.status, q.priority, q.not_before, q.attempts,
			          q.run_id, q.lease_owner, q.lease_expires_at, q.last_error, q.created_at, q.updated_at, q.completed_at
		)
		SELECT id, source_id, task_type, status, priority, not_before, attempts,
		       run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
		FROM claimed
		ORDER BY priority::BIGINT + FLOOR(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - created_at)) / 3600)::BIGINT DESC,
		         priority DESC, created_at ASC, id ASC
	`, sanitizeExploreLimit(batchLimit), run.ID, owner, seconds)
	if err != nil {
		return nil, nil, err
	}
	var tasks []ExploreQueueTask
	for rows.Next() {
		task, err := scanExploreQueueTask(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		tasks = append(tasks, *task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	if len(tasks) == 0 {
		run, err = updateExploreRun(tx, `UPDATE explore_fetch_runs SET status = 'done', completed_at = CURRENT_TIMESTAMP WHERE id = $1 RETURNING `, run.ID)
	} else {
		run, err = updateExploreRun(tx, `UPDATE explore_fetch_runs SET claimed_count = $2 WHERE id = $1 RETURNING `, run.ID, len(tasks))
	}
	if err != nil {
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
		FROM explore_fetch_queue
		WHERE run_id = $1 AND status = 'leased' AND lease_owner = $2 AND lease_expires_at > CURRENT_TIMESTAMP
		ORDER BY id
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

func (r *ExploreQueueRepository) Complete(taskID, runID int, owner string) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue SET status = 'done', completed_at = CURRENT_TIMESTAMP,
		lease_owner = NULL, lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND run_id = $2 AND status = 'leased' AND lease_owner = $3 AND lease_expires_at > CURRENT_TIMESTAMP
	`, taskID, runID, owner)
	return expectExploreLeaseTransition(result, err, taskID)
}

func (r *ExploreQueueRepository) Retry(taskID, runID int, owner string, cause error) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue
		SET status = 'pending', attempts = attempts + 1,
		    not_before = CURRENT_TIMESTAMP + (LEAST(3600, 60 * power(2, attempts)) * INTERVAL '1 second'),
		    run_id = NULL, lease_owner = NULL, lease_expires_at = NULL, last_error = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND run_id = $2 AND status = 'leased' AND lease_owner = $3 AND lease_expires_at > CURRENT_TIMESTAMP
	`, taskID, runID, owner, clipExploreError(cause))
	return expectExploreLeaseTransition(result, err, taskID)
}

func (r *ExploreQueueRepository) Invalidate(taskID, runID int, owner string, cause error) error {
	result, err := r.db.Exec(`
		UPDATE explore_fetch_queue SET status = 'invalid', completed_at = CURRENT_TIMESTAMP, last_error = $4,
		lease_owner = NULL, lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND run_id = $2 AND status = 'leased' AND lease_owner = $3 AND lease_expires_at > CURRENT_TIMESTAMP
	`, taskID, runID, owner, clipExploreError(cause))
	return expectExploreLeaseTransition(result, err, taskID)
}

// RecoverExpired reassigns the oldest expired lease set to a new owner while
// preserving every task's original run_id and that run's immutable quota.
// It owns a short transaction so run selection and lease reassignment cannot
// race a fresh ClaimRun or a second recovery dispatcher.
func (r *ExploreQueueRepository) RecoverExpired(newOwner string, leaseDuration time.Duration) (*ExploreFetchRun, []ExploreQueueTask, error) {
	if r.rawDB == nil {
		return nil, nil, errors.New("explore RecoverExpired requires a database handle")
	}
	seconds, err := exploreLeaseSeconds(leaseDuration)
	if err != nil {
		return nil, nil, err
	}
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
	run := &ExploreFetchRun{}
	err = tx.QueryRow(`
		SELECT run.id, run.window_at, run.status, run.claimed_count, run.started_at,
		       run.completed_at, run.worker_id, run.error_message, run.created_at
		FROM explore_fetch_runs run
		WHERE EXISTS (
			SELECT 1 FROM explore_fetch_queue task
			WHERE task.run_id=run.id AND task.status='leased'
			  AND task.lease_expires_at <= CURRENT_TIMESTAMP
		)
		ORDER BY run.window_at ASC, run.id ASC
		LIMIT 1 FOR UPDATE SKIP LOCKED
	`).Scan(&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID, &run.ErrorMessage, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	run, err = updateExploreRun(tx, `
		UPDATE explore_fetch_runs
		SET status='running', completed_at=NULL, worker_id=$2, error_message=NULL
		WHERE id=$1 RETURNING `, run.ID, newOwner)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(`
		WITH expired AS (
			SELECT id FROM explore_fetch_queue
			WHERE run_id=$1 AND status='leased' AND lease_expires_at <= CURRENT_TIMESTAMP
			ORDER BY id FOR UPDATE
		), recovered AS (
			UPDATE explore_fetch_queue task
			SET lease_owner = $2, lease_expires_at = CURRENT_TIMESTAMP + make_interval(secs => $3), updated_at = CURRENT_TIMESTAMP
			FROM expired WHERE task.id=expired.id
			RETURNING task.id, task.source_id, task.task_type, task.status, task.priority, task.not_before, task.attempts,
			          task.run_id, task.lease_owner, task.lease_expires_at, task.last_error, task.created_at, task.updated_at, task.completed_at
		)
		SELECT id, source_id, task_type, status, priority, not_before, attempts,
		       run_id, lease_owner, lease_expires_at, last_error, created_at, updated_at, completed_at
		FROM recovered ORDER BY id
	`, run.ID, newOwner, seconds)
	if err != nil {
		return nil, nil, err
	}
	var tasks []ExploreQueueTask
	for rows.Next() {
		task, err := scanExploreQueueTask(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		tasks = append(tasks, *task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if len(tasks) == 0 {
		return nil, nil, errors.New("expired explore run lost its recoverable tasks")
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return run, tasks, nil
}

func insertOrLockExploreRun(tx *sql.Tx, windowAt time.Time, owner string) (*ExploreFetchRun, bool, error) {
	run := &ExploreFetchRun{}
	err := tx.QueryRow(`
		INSERT INTO explore_fetch_runs (window_at, status, started_at, worker_id)
		VALUES ($1, 'running', CURRENT_TIMESTAMP, $2) ON CONFLICT (window_at) DO NOTHING
		RETURNING id, window_at, status, claimed_count, started_at, completed_at, worker_id, error_message, created_at
	`, windowAt, owner).Scan(&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID, &run.ErrorMessage, &run.CreatedAt)
	if err == nil {
		return run, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	err = tx.QueryRow(`
		SELECT id, window_at, status, claimed_count, started_at, completed_at, worker_id, error_message, created_at
		FROM explore_fetch_runs WHERE window_at = $1 FOR UPDATE
	`, windowAt).Scan(&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID, &run.ErrorMessage, &run.CreatedAt)
	return run, false, err
}

func updateExploreRun(tx *sql.Tx, prefix string, args ...interface{}) (*ExploreFetchRun, error) {
	run := &ExploreFetchRun{}
	err := tx.QueryRow(prefix+`id, window_at, status, claimed_count, started_at, completed_at, worker_id, error_message, created_at`, args...).Scan(
		&run.ID, &run.WindowAt, &run.Status, &run.ClaimedCount, &run.StartedAt, &run.CompletedAt, &run.WorkerID, &run.ErrorMessage, &run.CreatedAt)
	return run, err
}

func exploreLeaseSeconds(duration time.Duration) (float64, error) {
	if duration <= 0 {
		return 0, errors.New("explore lease duration must be positive")
	}
	return duration.Seconds(), nil
}
func sanitizeExploreLimit(limit int) int {
	if limit <= 0 || limit > maxExploreClaim {
		return maxExploreClaim
	}
	return limit
}
func sanitizeExplorePriority(priority int) int {
	if priority < 0 {
		return 0
	}
	if priority > maxExplorePriority {
		return maxExplorePriority
	}
	return priority
}
func scanExploreQueueTask(scanner interface{ Scan(...interface{}) error }) (*ExploreQueueTask, error) {
	task := &ExploreQueueTask{}
	err := scanner.Scan(&task.ID, &task.SourceID, &task.TaskType, &task.Status, &task.Priority, &task.NotBefore, &task.Attempts, &task.RunID, &task.LeaseOwner, &task.LeaseExpiresAt, &task.LastError, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
	return task, err
}
func expectExploreLeaseTransition(result sql.Result, err error, taskID int) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: task %d", ErrExploreLeaseNotHeld, taskID)
	}
	return nil
}
func clipExploreError(cause error) string {
	return ClipExploreError(cause)
}

// ClipExploreError bounds persisted errors without splitting UTF-8 runes.
func ClipExploreError(cause error) string {
	if cause == nil {
		return ""
	}
	const max = 1000
	s := cause.Error()
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
