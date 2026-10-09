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
// windows are registered together; an explicit re-add restores a removed browser provider.
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
		err = tx.QueryRowContext(ctx, `SELECT browser_only AND provider_kind='reddit_top' FROM explore_registry_providers WHERE provider_key=$1 FOR UPDATE`, key).Scan(&available)
		if err != nil {
			return "", err
		}
		if !available {
			return "", ErrRedditBrowserDisabled
		}
		if _, err = tx.ExecContext(ctx, `UPDATE explore_registry_providers SET enabled=true WHERE provider_key=$1`, key); err != nil {
			return "", err
		}
	}
	return name, tx.Commit()
}

type RedditSubreddit struct {
	Name          string     `json:"name"`
	Enabled       bool       `json:"enabled"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}

func (r *RedditBrowserRepository) ListSubreddits(ctx context.Context) ([]RedditSubreddit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT regexp_replace(provider_key,'^reddit-(.*)-top-(week|month)$','\1'),bool_and(enabled),max(last_success_at) FROM explore_registry_providers WHERE provider_kind='reddit_top' AND browser_only AND provider_key ~ '^reddit-[a-z0-9_]+-top-(week|month)$' GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RedditSubreddit{}
	for rows.Next() {
		var item RedditSubreddit
		if err := rows.Scan(&item.Name, &item.Enabled, &item.LastSuccessAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *RedditBrowserRepository) RemoveSubreddit(ctx context.Context, name string) error {
	name, err := explore.NormalizeSubreddit(name)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE explore_registry_providers SET enabled=false WHERE provider_kind='reddit_top' AND browser_only AND provider_key IN ($1,$2)`, "reddit-"+name+"-top-week", "reddit-"+name+"-top-month")
	return err
}
