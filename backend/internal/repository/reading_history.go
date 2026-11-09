package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

type ReadingHistoryItem struct {
	ID          int64     `json:"id"`
	ArticleID   *int      `json:"article_id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	SourceTitle string    `json:"source_title"`
	OpenedAt    time.Time `json:"opened_at"`
	Available   bool      `json:"available"`
}

type ReadingHistoryRepository struct {
	db Querier
}

func NewReadingHistoryRepository(db *sql.DB) *ReadingHistoryRepository {
	return &ReadingHistoryRepository{db: db}
}

func (r *ReadingHistoryRepository) WithCtx(c ctxkey.CtxGetter) *ReadingHistoryRepository {
	if value, ok := c.Get(ctxkey.Tx); ok {
		if db, ok := value.(Querier); ok {
			return &ReadingHistoryRepository{db: db}
		}
	}
	return r
}

func (r *ReadingHistoryRepository) Record(userID, articleID int, explore bool) error {
	query := `
		INSERT INTO reading_history(user_id,article_id)
		SELECT $1,a.id FROM articles a JOIN feeds f ON f.id=a.feed_id
		WHERE a.id=$2 AND f.owner_id=$1
		ON CONFLICT (article_id,user_id) WHERE article_id IS NOT NULL DO UPDATE
		SET opened_at=GREATEST(reading_history.opened_at,EXCLUDED.opened_at)
	`
	args := []any{userID, articleID}
	if explore {
		formalURLs, err := visibleFormalNormalizedURLs(r.db, userID)
		if err != nil {
			return err
		}
		query = exploreColdSourcesCTE + `
			INSERT INTO reading_history(user_id,explore_article_id,explore_article_key,explore_title,explore_source_title)
			SELECT $1,article.id,article.normalized_url,article.title,COALESCE(source.title,'')
			FROM explore_articles article
			JOIN recommended_feeds source ON source.id=article.source_id
			LEFT JOIN explore_article_states state ON state.user_id=$1 AND state.normalized_url=article.normalized_url
			WHERE article.id=$3 AND (` + exploreArticleVisibility + `)
			ON CONFLICT (explore_article_key,user_id) WHERE explore_article_key IS NOT NULL DO UPDATE
			SET opened_at=GREATEST(reading_history.opened_at,EXCLUDED.opened_at),
			    explore_article_id=CASE WHEN EXCLUDED.opened_at >= reading_history.opened_at
			        THEN EXCLUDED.explore_article_id ELSE reading_history.explore_article_id END,
			    explore_title=CASE WHEN EXCLUDED.opened_at >= reading_history.opened_at
			        THEN EXCLUDED.explore_title ELSE reading_history.explore_title END,
			    explore_source_title=CASE WHEN EXCLUDED.opened_at >= reading_history.opened_at
			        THEN EXCLUDED.explore_source_title ELSE reading_history.explore_source_title END
		`
		args = []any{userID, pq.Array(formalURLs), articleID}
	}
	result, err := r.db.Exec(query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *ReadingHistoryRepository) List(userID, limit int, before *time.Time, beforeID int64) ([]ReadingHistoryItem, error) {
	formalURLs, err := visibleFormalNormalizedURLs(r.db, userID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(exploreColdSourcesCTE+`
		SELECT history.id, entry.article_id, entry.kind, entry.title, entry.source_title, history.opened_at, entry.available
		FROM reading_history history
		JOIN LATERAL (
			SELECT a.id AS article_id, CASE WHEN f.feed_type='clip' THEN 'clip' ELSE 'article' END AS kind,
			       a.title, COALESCE(f.title,'') AS source_title, true AS available
			FROM articles a JOIN feeds f ON f.id=a.feed_id
			WHERE a.id=history.article_id AND f.owner_id=$1
			UNION ALL
			SELECT article.id, 'explore', history.explore_title, history.explore_source_title,
			       article.id IS NOT NULL AND COALESCE((`+exploreArticleVisibility+`), false)
			FROM (SELECT history.explore_article_id AS id) entry
			LEFT JOIN explore_articles article ON article.id=entry.id
			LEFT JOIN recommended_feeds source ON source.id=article.source_id
			LEFT JOIN explore_article_states state ON state.user_id=$1 AND state.normalized_url=article.normalized_url
			WHERE history.explore_article_key IS NOT NULL
		) entry ON true
		WHERE history.user_id=$1 AND ($3::timestamptz IS NULL OR (history.opened_at,history.id) < ($3,$4))
		ORDER BY history.opened_at DESC, history.id DESC
		LIMIT $5
	`, userID, pq.Array(formalURLs), before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ReadingHistoryItem, 0, limit)
	for rows.Next() {
		var item ReadingHistoryItem
		if err := rows.Scan(&item.ID, &item.ArticleID, &item.Kind, &item.Title, &item.SourceTitle, &item.OpenedAt, &item.Available); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
