package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

var ErrSummaryInProgress = errors.New("summary_in_progress")

type SummaryJob struct {
	State          string     `json:"state"`
	LastError      string     `json:"last_error,omitempty"`
	RetryAt        *time.Time `json:"retry_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
}

// SetSummaryJobDB supplies the privileged autocommit connection. Request RLS
// transactions must never own admission leases across provider network calls.
func (r *ArticleRepository) SetSummaryJobDB(db *sql.DB) { r.summaryDB = db }

func (r *ArticleRepository) ClaimSummary(ctx context.Context, id int, manual bool) (string, error) {
	if r.summaryDB == nil {
		return "", errors.New("summary job store unavailable")
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random[:])
	var claimed string
	err := r.summaryDB.QueryRowContext(ctx, `INSERT INTO article_summary_jobs(article_id,state,lease_token,lease_expires_at)
 VALUES($1,'running',$2,clock_timestamp()+interval '10 minutes')
 ON CONFLICT(article_id) DO UPDATE SET state='running',lease_token=EXCLUDED.lease_token,
 lease_expires_at=EXCLUDED.lease_expires_at,attempts=article_summary_jobs.attempts+1,last_error='',retry_at=NULL,updated_at=clock_timestamp()
 WHERE (article_summary_jobs.state!='running' OR article_summary_jobs.lease_expires_at<=clock_timestamp())
 AND ($3 OR article_summary_jobs.retry_at IS NULL OR article_summary_jobs.retry_at<=clock_timestamp())
 RETURNING lease_token`, id, token, manual).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSummaryInProgress
	}
	return claimed, err
}

func (r *ArticleRepository) GetSummaryJob(ctx context.Context, id int) (*SummaryJob, error) {
	if r.summaryDB == nil {
		return nil, errors.New("summary job store unavailable")
	}
	// A crashed regeneration may retain old summaries and never enter automatic
	// backfill. Reconcile its expired lease when the reader checks job status.
	if _, err := r.summaryDB.ExecContext(ctx, `UPDATE article_summary_jobs SET state='failed',last_error='总结任务已中断，请重试',lease_expires_at=NULL,retry_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE article_id=$1 AND state='running' AND lease_expires_at<=clock_timestamp()`, id); err != nil {
		return nil, err
	}
	job := new(SummaryJob)
	err := r.summaryDB.QueryRowContext(ctx, `SELECT state,last_error,retry_at,lease_expires_at FROM article_summary_jobs WHERE article_id=$1`, id).Scan(&job.State, &job.LastError, &job.RetryAt, &job.LeaseExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

// CompleteSummary locks the lease before checking content; superseded or expired
// owners cannot overwrite either summaries or a newer owner's job state.
func (r *ArticleRepository) CompleteSummary(id int, token, content, brief, detailed string) (bool, error) {
	if strings.TrimSpace(brief) == "" || strings.TrimSpace(detailed) == "" {
		return false, errors.New("AI 返回了空总结，请重试")
	}
	var saved bool
	err := r.db.QueryRow(`WITH lease AS MATERIALIZED (
 SELECT article_id FROM article_summary_jobs WHERE article_id=$1 AND lease_token=$2 AND state='running' AND lease_expires_at>clock_timestamp() FOR UPDATE
 ), saved AS (
 UPDATE articles SET summary_brief=$4,summary_detailed=$5,
 processing_state=CASE WHEN processing_state='processing' THEN 'ready' ELSE processing_state END
 WHERE id=$1 AND content IS NOT DISTINCT FROM $3 AND EXISTS(SELECT 1 FROM lease) RETURNING id
 ), finished AS (
 UPDATE article_summary_jobs SET state='done',lease_expires_at=NULL,retry_at=NULL,last_error='',attempts=0,updated_at=clock_timestamp()
 WHERE article_id IN(SELECT id FROM saved) AND lease_token=$2 RETURNING article_id)
 SELECT EXISTS(SELECT 1 FROM finished)`, id, token, content, brief, detailed).Scan(&saved)
	return saved, err
}

func (r *ArticleRepository) FailSummary(id int, token string, cause error) error {
	if r.summaryDB == nil {
		return errors.New("summary job store unavailable")
	}
	// Provider errors can include credential-bearing URLs or internal addresses.
	// Store only safe reader-facing reasons; callers retain the original error.
	message := "总结生成失败，请重试"
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		message = "总结已中断，请重试"
	}
	var retry *time.Time
	var limit *taskbudget.LimitError
	if errors.As(cause, &limit) {
		retry = limit.RetryAt
		message = limit.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.summaryDB.ExecContext(ctx, `UPDATE article_summary_jobs SET state='failed',last_error=$3,lease_expires_at=NULL,
 retry_at=GREATEST(clock_timestamp()+ interval '1 minute' * power(2,LEAST(GREATEST(attempts-1,0),8)),COALESCE($4::timestamptz,clock_timestamp())),updated_at=clock_timestamp()
 WHERE article_id=$1 AND lease_token=$2 AND state='running'`, id, token, message, retry)
	if err != nil {
		return fmt.Errorf("record summary failure: %w", err)
	}
	return nil
}
