package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
)

var ErrRedditBrowserDisabled = errors.New("Reddit browser provider unavailable or disabled")

type RedditBrowserResult struct {
	Accepted  int                    `json:"accepted"`
	Duplicate bool                   `json:"duplicate"`
	Stats     explore.RedditTopStats `json:"stats"`
}

type RedditBrowserRepository struct{ db *sql.DB }

func NewRedditBrowserRepository(db *sql.DB) *RedditBrowserRepository {
	return &RedditBrowserRepository{db: db}
}

// Ingest atomically records observations and validation jobs. A retried batch
// cannot renew stale evidence or leave partially accepted candidates behind.
func (r *RedditBrowserRepository) Ingest(ctx context.Context, b explore.RedditBrowserBatch, now time.Time) (RedditBrowserResult, error) {
	result := RedditBrowserResult{}
	key, err := b.ProviderKey()
	if err != nil {
		return result, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var id int
	var provider explore.Provider
	var success sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,endpoint,COALESCE(topic,''),last_success_at FROM explore_registry_providers WHERE provider_key=$1 AND provider_kind='reddit_top' AND browser_only AND enabled FOR UPDATE`, key).Scan(&id, &provider.Endpoint, &provider.Topic, &success)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrRedditBrowserDisabled
	}
	if err != nil {
		return result, err
	}
	candidates, stats, err := b.Parse(provider, now)
	if err != nil {
		return result, err
	}
	result.Stats = stats
	if success.Valid && !b.CapturedAt.After(success.Time) {
		result.Duplicate = true
		return result, tx.Commit()
	}
	registry := &ExploreRegistryRepository{db: tx}
	queue := NewExploreQueueRepository(r.db).WithQuerier(tx)
	for _, candidate := range candidates {
		id, err := registry.UpsertCandidate(id, candidate, b.CapturedAt)
		if err != nil {
			return result, err
		}
		if _, err := queue.Enqueue(id, ExploreTaskValidateSource, ExplorePriorityStructuredProvider); err != nil {
			return result, err
		}
		result.Accepted++
	}
	if err := registry.RecordSuccess(id, b.CapturedAt, "", ""); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// RegisterSubreddit is called only after an explicit administrator action. Both
// windows are registered together; existing disabled providers stay disabled.
func (r *RedditBrowserRepository) RegisterSubreddit(ctx context.Context, name string) (string, error) {
	name, err := explore.NormalizeSubreddit(name)
	if err != nil {
		return "", err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for _, period := range []string{"week", "month"} {
		key, _ := (explore.RedditBrowserBatch{Subreddit: name, Period: period}).ProviderKey()
		_, err = tx.ExecContext(ctx, `INSERT INTO explore_registry_providers(provider_key,provider_kind,endpoint,topic,sync_interval_minutes,browser_only)
    VALUES($1,'reddit_top',$2,'general',360,true) ON CONFLICT(provider_key) DO NOTHING`, key, explore.RedditTopEndpoint(name, period, 100))
		if err != nil {
			return "", err
		}
		var available bool
		err = tx.QueryRowContext(ctx, `SELECT enabled AND browser_only AND provider_kind='reddit_top' FROM explore_registry_providers WHERE provider_key=$1 FOR UPDATE`, key).Scan(&available)
		if err != nil {
			return "", err
		}
		if !available {
			return "", ErrRedditBrowserDisabled
		}
	}
	return name, tx.Commit()
}
