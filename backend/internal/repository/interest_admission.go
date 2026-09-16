package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

var ErrInterestQuota = errors.New("interest quota exceeded")

// ReserveManual atomically checks attempt quotas and persists a pending row.
// Pending/failed attempts count, and requested_at is immutable on completion.
func (r *UserInterestRepository) ReserveManual(ctx context.Context, userID int, modelName string, daily, monthly int) (int, error) {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true)`, userID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(817234,$1)`, userID); err != nil {
		return 0, err
	}
	// A crashed process cannot leave the user permanently pending. Normal jobs
	// have a five-minute timeout; ten minutes is a conservative recovery bound.
	if _, err = tx.ExecContext(ctx, `UPDATE user_insights SET status='failed',error_msg='任务超时，请重试' WHERE user_id=$1 AND status='pending' AND requested_at<clock_timestamp()-INTERVAL '10 minutes'`, userID); err != nil {
		return 0, err
	}
	var today, month int
	var pending bool
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE triggered_by='manual' AND requested_at>clock_timestamp()-INTERVAL '24 hours'),count(*) FILTER(WHERE triggered_by='manual' AND requested_at>clock_timestamp()-INTERVAL '30 days'),COALESCE(bool_or(status='pending'),false) FROM user_insights WHERE user_id=$1`, userID).Scan(&today, &month, &pending); err != nil {
		return 0, err
	}
	if pending {
		return 0, ErrPendingExists
	}
	if today >= daily || month >= monthly {
		return 0, ErrInterestQuota
	}
	var id int
	err = tx.QueryRowContext(ctx, `INSERT INTO user_insights(user_id,content,status,triggered_by,model) VALUES($1,NULL,'pending','manual',NULLIF($2,'')) ON CONFLICT DO NOTHING RETURNING id`, userID, modelName).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPendingExists
	}
	if err != nil {
		return 0, err
	}
	if err = commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *UserInterestRepository) finishForUser(userID int, fn func(*UserInterestRepository) error) error {
	pool, ok := r.db.(*sql.DB)
	if !ok {
		return errors.New("async completion requires independent database pool")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true)`, userID); err != nil {
		return err
	}
	if err = fn(&UserInterestRepository{db: tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *UserInterestRepository) MarkFailedForUser(id, userID int, message string) error {
	return r.finishForUser(userID, func(scoped *UserInterestRepository) error { return scoped.MarkFailed(id, message) })
}
func (r *UserInterestRepository) MarkDoneForUser(id, userID int, content string, recs []model.RecommendationDirection) error {
	return r.finishForUser(userID, func(scoped *UserInterestRepository) error { return scoped.MarkDoneWithRecs(id, content, recs) })
}
