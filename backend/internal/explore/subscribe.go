package explore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/lib/pq"
)

const (
	MaxSubscribeSources  = 12
	SubscribeSnapshotAge = 30 * 24 * time.Hour
)

var (
	ErrInvalidSubscribeRequest    = errors.New("invalid explore subscription request")
	ErrSubscribeSourceUnavailable = errors.New("explore source is not available to this user")
)

// Querier is the database surface needed by subscription promotion. Both
// *sql.DB and *sql.Tx implement it, which lets HTTP requests reuse their RLS
// transaction while workers and tests can start a short transaction here.
type Querier interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type SubscribeResult struct {
	SourceID       int  `json:"source_id"`
	FeedID         int  `json:"feed_id"`
	Created        bool `json:"created"`
	CopiedArticles int  `json:"copied_articles"`
}

type SubscribeService struct {
	db  Querier
	now func() time.Time
}

func NewSubscribeService(db Querier, clocks ...func() time.Time) *SubscribeService {
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &SubscribeService{db: db, now: now}
}

func (s *SubscribeService) WithQuerier(db Querier) *SubscribeService {
	if s == nil {
		return nil
	}
	return &SubscribeService{db: db, now: s.now}
}

func (s *SubscribeService) WithCtx(c interface{ Get(string) (any, bool) }) *SubscribeService {
	if s == nil {
		return nil
	}
	if value, ok := c.Get(ctxkey.Tx); ok {
		if db, ok := value.(Querier); ok {
			return s.WithQuerier(db)
		}
	}
	return s
}

func (s *SubscribeService) SubscribeOne(userID, sourceID int) (SubscribeResult, error) {
	results, err := s.Subscribe(userID, []int{sourceID})
	if err != nil {
		return SubscribeResult{}, err
	}
	return results[0], nil
}

// Subscribe validates the complete selection before mutating anything, then
// promotes every source and its cached articles in one transaction.
func (s *SubscribeService) Subscribe(userID int, sourceIDs []int) ([]SubscribeResult, error) {
	if s == nil || s.db == nil || userID <= 0 || len(sourceIDs) == 0 || len(sourceIDs) > MaxSubscribeSources {
		return nil, ErrInvalidSubscribeRequest
	}
	seen := make(map[int]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID <= 0 {
			return nil, ErrInvalidSubscribeRequest
		}
		if _, duplicate := seen[sourceID]; duplicate {
			return nil, ErrInvalidSubscribeRequest
		}
		seen[sourceID] = struct{}{}
	}

	tx, commit, rollback, err := subscribeTxOrBegin(s.db)
	if err != nil {
		return nil, err
	}
	defer rollback()

	sources, err := loadPromotableSources(tx, userID, sourceIDs, s.now())
	if err != nil {
		return nil, err
	}
	if len(sources) != len(sourceIDs) {
		return nil, ErrSubscribeSourceUnavailable
	}

	// Mutate in one stable order to avoid reversed batch requests deadlocking
	// on owner/url uniqueness locks. Results are restored to request order.
	bySource := make(map[int]SubscribeResult, len(sources))
	for _, source := range sources {
		feed, created, err := GetOrCreateOwnerScopedFeed(tx, userID, source.URL, source.Title, source.FeedType)
		if err != nil {
			return nil, err
		}
		copied := 0
		// Shared feeds and their articles are global state. A user subscription
		// only reuses that state; it must never use candidate-cache data to
		// insert, refresh, or invalidate summaries on the shared feed.
		if feed.OwnerID != nil {
			copied, err = copyExploreArticles(tx, userID, source.ID, feed.ID)
			if err != nil {
				return nil, err
			}
		}
		bySource[source.ID] = SubscribeResult{
			SourceID: source.ID, FeedID: feed.ID, Created: created, CopiedArticles: copied,
		}
	}
	results := make([]SubscribeResult, len(sourceIDs))
	for index, sourceID := range sourceIDs {
		results[index] = bySource[sourceID]
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return results, nil
}

type promotableSource struct {
	ID       int
	URL      string
	Title    string
	FeedType string
}

func loadPromotableSources(db Querier, userID int, sourceIDs []int, now time.Time) ([]promotableSource, error) {
	rows, err := db.Query(`
		SELECT source.id, source.url, source.title, COALESCE(NULLIF(source.feed_type,''),'rss')
		FROM recommended_feeds source
		WHERE source.id=ANY($2)
		  AND source.validation_status='valid'
		  AND source.is_broken=false
		  AND source.merged_into_source_id IS NULL
		  AND EXISTS (
		      SELECT 1
		      FROM explore_batch_sources batch_source
		      JOIN explore_batches batch
		        ON batch.id=batch_source.batch_id AND batch.user_id=batch_source.user_id
		      WHERE batch_source.user_id=$1
		        AND batch_source.source_id=source.id
		        AND batch.status='done'
		        AND batch.completed_at >= $3
		  )
		ORDER BY source.id
		FOR SHARE OF source`, userID, pq.Array(sourceIDs), now.Add(-SubscribeSnapshotAge))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := make([]promotableSource, 0, len(sourceIDs))
	for rows.Next() {
		var source promotableSource
		if err := rows.Scan(&source.ID, &source.URL, &source.Title, &source.FeedType); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

const copyExploreArticleCandidatesSQL = `
	SELECT article.title,article.url,article.content,article.published_at,article.fetched_at
	FROM explore_articles article
	JOIN feeds target ON target.id=$2
	WHERE article.source_id=$1
	  AND target.owner_id=$3
	ORDER BY article.id
	FOR SHARE OF target`

const copyExploreArticleUpsertSQL = `
	INSERT INTO articles
		(feed_id,title,url,content,published_at,fetched_at,word_count,reading_minutes)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	ON CONFLICT (feed_id,url) WHERE parent_article_id IS NULL AND NOT is_clip
	DO UPDATE SET
		summary_brief=CASE WHEN articles.content IS DISTINCT FROM EXCLUDED.content
			THEN NULL ELSE articles.summary_brief END,
		summary_detailed=CASE WHEN articles.content IS DISTINCT FROM EXCLUDED.content
			THEN NULL ELSE articles.summary_detailed END,
		title=EXCLUDED.title,
		content=EXCLUDED.content,
		published_at=EXCLUDED.published_at,
		fetched_at=EXCLUDED.fetched_at,
		word_count=EXCLUDED.word_count,
		reading_minutes=EXCLUDED.reading_minutes`

type exploreArticleCopyCandidate struct {
	title       string
	url         string
	content     sql.NullString
	publishedAt sql.NullTime
	fetchedAt   time.Time
}

func copyExploreArticles(db Querier, userID, sourceID, feedID int) (int, error) {
	rows, err := db.Query(copyExploreArticleCandidatesSQL, sourceID, feedID, userID)
	if err != nil {
		return 0, err
	}
	candidates := make([]exploreArticleCopyCandidate, 0)
	for rows.Next() {
		var candidate exploreArticleCopyCandidate
		if err := rows.Scan(
			&candidate.title, &candidate.url, &candidate.content,
			&candidate.publishedAt, &candidate.fetchedAt,
		); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	copied := 0
	for _, candidate := range candidates {
		wordCount, readingMinutes := rss.ComputeMetrics(candidate.content.String)
		var content interface{}
		if candidate.content.Valid {
			content = candidate.content.String
		}
		var publishedAt interface{}
		if candidate.publishedAt.Valid {
			publishedAt = candidate.publishedAt.Time
		}
		args := []interface{}{
			feedID, candidate.title, candidate.url, content, publishedAt,
			candidate.fetchedAt, wordCount, readingMinutes,
		}
		result, err := db.Exec(copyExploreArticleUpsertSQL, args...)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		copied += int(count)
	}
	return copied, nil
}

// GetOrCreateOwnerScopedFeed reuses a visible shared feed first. Otherwise it
// creates one feed per owner+URL and safely re-reads the winner of a race.
func GetOrCreateOwnerScopedFeed(db Querier, ownerID int, url, title, feedType string) (*model.Feed, bool, error) {
	url = strings.TrimSpace(url)
	title = strings.TrimSpace(title)
	feedType = strings.TrimSpace(feedType)
	if db == nil || ownerID <= 0 || url == "" {
		return nil, false, ErrInvalidSubscribeRequest
	}
	if feedType == "" {
		feedType = "rss"
	}
	if shared, err := scanOwnerScopedFeed(db.QueryRow(`
		SELECT id,url,title,owner_id,feed_type,fetch_interval_minutes,is_active,status,created_at
		FROM feeds WHERE owner_id IS NULL AND url=$1
		ORDER BY id LIMIT 1 FOR SHARE`, url)); err == nil {
		return shared, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	owner := ownerID
	feed := &model.Feed{URL: url, Title: title, OwnerID: &owner, FeedType: feedType, FetchIntervalMin: 60, IsActive: true, Status: "active"}
	err := db.QueryRow(`
		INSERT INTO feeds (url,title,fetch_interval_minutes,is_active,owner_id,feed_type,status,expand_links)
		VALUES ($1,$2,60,true,$3,$4,'active',false)
		ON CONFLICT ((COALESCE(owner_id,0)),url) DO NOTHING
		RETURNING id,created_at`, url, title, ownerID, feedType).Scan(&feed.ID, &feed.CreatedAt)
	if err == nil {
		return feed, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	feed, err = scanOwnerScopedFeed(db.QueryRow(`
		SELECT id,url,title,owner_id,feed_type,fetch_interval_minutes,is_active,status,created_at
		FROM feeds WHERE owner_id=$1 AND url=$2
		FOR SHARE`, ownerID, url))
	if err != nil {
		return nil, false, err
	}
	return feed, false, nil
}

func scanOwnerScopedFeed(row *sql.Row) (*model.Feed, error) {
	var feed model.Feed
	var title, feedType, status sql.NullString
	var ownerID sql.NullInt64
	if err := row.Scan(&feed.ID, &feed.URL, &title, &ownerID, &feedType, &feed.FetchIntervalMin, &feed.IsActive, &status, &feed.CreatedAt); err != nil {
		return nil, err
	}
	feed.Title, feed.FeedType, feed.Status = title.String, feedType.String, status.String
	if ownerID.Valid {
		owner := int(ownerID.Int64)
		feed.OwnerID = &owner
	}
	return &feed, nil
}

func subscribeTxOrBegin(db Querier) (Querier, func() error, func() error, error) {
	if tx, ok := db.(*sql.Tx); ok {
		noop := func() error { return nil }
		return tx, noop, noop, nil
	}
	pool, ok := db.(*sql.DB)
	if !ok {
		return nil, nil, nil, fmt.Errorf("subscribe txOrBegin: Querier is neither *sql.Tx nor *sql.DB")
	}
	tx, err := pool.Begin()
	if err != nil {
		return nil, nil, nil, err
	}
	return tx, tx.Commit, tx.Rollback, nil
}
