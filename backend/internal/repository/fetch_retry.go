package repository

import (
	"context"
	"fmt"
)

// Schedule from the database clock. Clamp the next attempt to the deadline;
// selectors exclude the deadline itself. Cap the exponent before computing it.
const retryDelaySQL = "(interval '2 minutes' * power(2, least(%s, 11)))"

var refetchRetryDelay = fmt.Sprintf(retryDelaySQL, "CASE WHEN refetch_retry_started_at IS NULL THEN 0 ELSE coalesce(refetch_attempts,0) END")

func (r *ArticleRepository) DeferContentRetry(id int) error {
	_, err := r.db.Exec(contentRetrySQL, id)
	return err
}

func (r *ArticleRepository) DeferLinkFetchRetry(id int) error {
	_, err := r.db.Exec(linkRetrySQL, id)
	return err
}

func (r *ArticleRepository) DeferTranscriptRetry(id int, providerPending bool) error {
	_, err := r.db.Exec(transcriptRetrySQL, id, providerPending)
	return err
}

// SET expressions see the previous row, so the current failure count is the
// exponent for the new delay. Legacy content failures start a fresh schedule
// only if they were still eligible under the old attempt limit.
var contentRetrySQL = fmt.Sprintf(`
	UPDATE articles
	SET refetch_next_attempt_at = least(
	        coalesce(refetch_retry_started_at, statement_timestamp()) + interval '72 hours',
	        statement_timestamp() + %s),
	    refetch_attempts = CASE WHEN refetch_retry_started_at IS NULL THEN 1 ELSE refetch_attempts + 1 END,
	    refetch_retry_started_at = coalesce(refetch_retry_started_at, statement_timestamp())
	WHERE id=$1 AND coalesce(processing_state, 'ready') NOT IN ('stub','queued','processing','failed')
	  AND (refetch_next_attempt_at IS NULL OR refetch_next_attempt_at <= statement_timestamp())`,
	refetchRetryDelay)

var linkRetrySQL = fmt.Sprintf(`
	UPDATE articles
	SET refetch_next_attempt_at = least(
	        coalesce(refetch_retry_started_at, statement_timestamp()) + interval '72 hours',
	        statement_timestamp() + %s),
	    refetch_attempts = CASE WHEN refetch_retry_started_at IS NULL THEN 1 ELSE refetch_attempts + 1 END,
	    refetch_retry_started_at = coalesce(refetch_retry_started_at, statement_timestamp())
	WHERE id=$1 AND parent_article_id IS NOT NULL AND processing_state='processing'
	  AND (refetch_next_attempt_at IS NULL OR refetch_next_attempt_at <= statement_timestamp())`,
	refetchRetryDelay)

var transcriptRetrySQL = fmt.Sprintf(`
	UPDATE articles
	SET transcript_next_attempt_at = least(
	        coalesce(transcript_retry_started_at, statement_timestamp()) + interval '72 hours',
	        statement_timestamp() + CASE WHEN $2 THEN interval '2 minutes' ELSE %s END),
	    transcript_retry_attempts = CASE WHEN $2 THEN transcript_retry_attempts ELSE transcript_retry_attempts + 1 END,
	    transcript_retry_started_at = coalesce(transcript_retry_started_at, statement_timestamp())
	WHERE id=$1 AND transcript_fetched_at IS NULL
	  AND (transcript_next_attempt_at IS NULL OR transcript_next_attempt_at <= statement_timestamp())`,
	fmt.Sprintf(retryDelaySQL, "transcript_retry_attempts"))

// FetchRetryDue rechecks the schedule after a task has waited for a worker slot.
func (r *ArticleRepository) FetchRetryDue(ctx context.Context, id int, transcript bool) (bool, error) {
	var due bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM articles a
			WHERE a.id=$1 AND CASE WHEN $2 THEN
				a.transcript_fetched_at IS NULL
				AND (a.transcript_next_attempt_at IS NULL OR a.transcript_next_attempt_at <= statement_timestamp())
				AND (a.transcript_retry_started_at IS NULL OR a.transcript_retry_started_at > statement_timestamp() - interval '72 hours')
			ELSE
				(a.refetch_next_attempt_at IS NULL OR a.refetch_next_attempt_at <= statement_timestamp())
				AND (a.refetch_retry_started_at IS NULL OR a.refetch_retry_started_at > statement_timestamp() - interval '72 hours')
			END
		)`, id, transcript).Scan(&due)
	return due, err
}
