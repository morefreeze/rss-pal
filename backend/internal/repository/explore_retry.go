package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
)

func stoppedExploreSource(state string) bool {
	return state == "unavailable" || state == "ineligible" || state == "retry_exhausted"
}

// Enqueue and processors take the source lock before task locks. Stop and retry
// decisions therefore cannot be bypassed by a concurrent rediscovery.
func (r *ExploreQueueRepository) sourceCanEnqueue(sourceID int) (bool, error) {
	var state string
	var due bool
	err := r.db.QueryRow(`SELECT fetch_state,(next_retry_at IS NULL OR next_retry_at<=CURRENT_TIMESTAMP) AND merged_into_source_id IS NULL FROM recommended_feeds WHERE id=$1 FOR UPDATE`, sourceID).Scan(&state, &due)
	return !stoppedExploreSource(state) && due, err
}

func (r *ExploreQueueRepository) retrySource(taskID, runID int, token string, cause error) error {
	if db, ok := r.db.(*sql.DB); ok {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = r.WithQuerier(tx).retrySource(taskID, runID, token, cause); err != nil {
			return err
		}
		return tx.Commit()
	}
	var sourceID int
	if err := r.db.QueryRow(`SELECT source_id FROM explore_fetch_queue WHERE id=$1`, taskID).Scan(&sourceID); err != nil {
		return err
	}
	var state string
	var failures int
	if err := r.db.QueryRow(`SELECT fetch_state,fetch_failures FROM recommended_feeds WHERE id=$1 FOR UPDATE`, sourceID).Scan(&state, &failures); err != nil {
		return err
	}
	var owned bool
	if err := r.db.QueryRow(`SELECT status='leased' AND run_id=$2 AND lease_token=$3 AND lease_expires_at>CURRENT_TIMESTAMP FROM explore_fetch_queue WHERE id=$1 FOR UPDATE`, taskID, runID, token).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return ErrExploreLeaseLost
	}
	if stoppedExploreSource(state) {
		return r.Invalidate(taskID, runID, token, errors.New("source automatic attempts stopped"))
	}
	failures++
	if failures > 6 {
		if _, err := r.db.Exec(`UPDATE recommended_feeds SET fetch_state='retry_exhausted',is_broken=true,health_score=0,fetch_failures=$2,next_retry_at=NULL,last_error=$3 WHERE id=$1`, sourceID, failures, clipExploreError(cause)); err != nil {
			return err
		}
		if err := r.Invalidate(taskID, runID, token, cause); err != nil {
			return err
		}
		return settleStoppedExploreTasks(r.db, sourceID)
	}
	// +/-10% jitter. Retry-After is a lower bound, never an earlier retry.
	seconds := []int{3600, 14400, 57600, 172800, 691200, 2764800}[failures-1]
	var retryAfter any
	if at := explore.RetryAfter(cause); !at.IsZero() {
		retryAfter = at
	}
	var next time.Time
	if err := r.db.QueryRow(`UPDATE recommended_feeds SET fetch_state='retry_wait',fetch_failures=$2,next_retry_at=GREATEST(CURRENT_TIMESTAMP+($3::double precision*(0.9+random()*0.2))*INTERVAL '1 second',$4::timestamp),last_error=$5 WHERE id=$1 RETURNING next_retry_at`, sourceID, failures, seconds, retryAfter, clipExploreError(cause)).Scan(&next); err != nil {
		return err
	}
	result, err := r.db.Exec(`UPDATE explore_fetch_queue SET status='pending',attempts=attempts+1,not_before=$4,run_id=NULL,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,last_error=$5,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND run_id=$2 AND status='leased' AND lease_token=$3 AND lease_expires_at>CURRENT_TIMESTAMP`, taskID, runID, token, next, clipExploreError(cause))
	if err = expectExploreLeaseTransition(result, err, taskID); err != nil {
		return err
	}
	_, err = r.db.Exec(`UPDATE explore_fetch_queue SET not_before=GREATEST(not_before,$2) WHERE source_id=$1 AND status='pending'`, sourceID, next)
	return err
}

func settleStoppedExploreTasks(db Querier, sourceID int) error {
	_, err := db.Exec(`UPDATE explore_fetch_queue SET status='invalid',completed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP,last_error=COALESCE(NULLIF(last_error,''),'source automatic attempts stopped') WHERE source_id=$1 AND status='pending'`, sourceID)
	return err
}

// Already-leased siblings can encounter a deadline set by a prior outcome.
// Deferral releases their lease without consuming another failure.
func (r *ExploreQueueRepository) deferSourceTask(task ExploreQueueTask) error {
	tx, err := r.rawDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	var next *time.Time
	if err := tx.QueryRow(`SELECT fetch_state,next_retry_at FROM recommended_feeds WHERE id=$1 FOR UPDATE`, task.SourceID).Scan(&state, &next); err != nil {
		return err
	}
	token, err := exploreTaskLeaseToken(task)
	if err != nil {
		return err
	}
	if stoppedExploreSource(state) {
		if err := r.WithQuerier(tx).Invalidate(task.ID, *task.RunID, token, errors.New("source automatic attempts stopped")); err != nil {
			return err
		}
	} else {
		result, err := tx.Exec(`UPDATE explore_fetch_queue SET status='pending',not_before=COALESCE($4,CURRENT_TIMESTAMP),run_id=NULL,lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND run_id=$2 AND lease_token=$3 AND status='leased' AND lease_expires_at>CURRENT_TIMESTAMP`, task.ID, *task.RunID, token, next)
		if err := expectExploreLeaseTransition(result, err, task.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
