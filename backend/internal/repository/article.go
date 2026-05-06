package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

type ArticleRepository struct {
	db *sql.DB
}

func NewArticleRepository(db *sql.DB) *ArticleRepository {
	return &ArticleRepository{db: db}
}

func (r *ArticleRepository) scanArticle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, feedTitle sql.NullString
		var isRead sql.NullBool
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &feedTitle, &isRead)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		articles = append(articles, a)
	}
	return articles, nil
}

func (r *ArticleRepository) scanArticleNoFeedTitle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed sql.NullString
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		articles = append(articles, a)
	}
	return articles, nil
}

func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int) ([]model.Article, error) {
	query := `SELECT articles.id, articles.feed_id, articles.title, articles.url, articles.content, articles.published_at, articles.summary_brief, articles.summary_detailed, articles.fetched_at, feeds.title as feed_title, COALESCE(rp.is_completed, false) as is_read
FROM articles
JOIN feeds ON articles.feed_id = feeds.id
LEFT JOIN reading_progress rp ON articles.id = rp.article_id AND rp.user_id = $1`
	args := []interface{}{userID}
	conditions := []string{}
	argIdx := 2

	// Only return articles from feeds visible to this user (shared feeds or user's own feeds)
	conditions = append(conditions, "(feeds.owner_id IS NULL OR feeds.owner_id = $1)")

	if feedID != nil {
		conditions = append(conditions, fmt.Sprintf("articles.feed_id = $%d", argIdx))
		args = append(args, *feedID)
		argIdx++
	}

	if unreadOnly {
		conditions = append(conditions, "COALESCE(rp.is_completed, false) = false")
	}

	if savedOnly {
		query += fmt.Sprintf(`
LEFT JOIN user_preferences up_save ON articles.id = up_save.article_id AND up_save.user_id = $1 AND up_save.signal_type = 'save'`)
		conditions = append(conditions, fmt.Sprintf("up_save.signal_value = $%d", argIdx))
		args = append(args, 1.0)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += " AND " + conditions[i]
		}
	}

	query += fmt.Sprintf(" ORDER BY COALESCE(articles.published_at, articles.fetched_at) DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanArticle(rows)
}

func (r *ArticleRepository) GetByID(id, userID int) (*model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, f.title as feed_title
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle sql.NullString
	err := r.db.QueryRow(query, id, userID).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &feedTitle)
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	return &a, nil
}

func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at) VALUES ($1, $2, $3, $4, $5) RETURNING id, fetched_at`
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt).Scan(&article.ID, &article.FetchedAt)
}

func (r *ArticleRepository) Exists(feedID int, url string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM articles WHERE feed_id = $1 AND url = $2)`
	var exists bool
	err := r.db.QueryRow(query, feedID, url).Scan(&exists)
	return exists, err
}

// FindByOwnerAndURL returns the article matching exactURL within any feed
// owned by ownerID, or (nil, nil) if no match. Caller is responsible for
// passing a normalized URL (see util.NormalizeURL).
func (r *ArticleRepository) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE f.owner_id = $1 AND a.url = $2
		LIMIT 1
	`
	var a model.Article
	var content, summaryBrief, summaryDetailed sql.NullString
	err := r.db.QueryRow(query, ownerID, exactURL).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	return &a, nil
}

func (r *ArticleRepository) UpdateSummary(id int, summaryBrief, summaryDetailed string) error {
	query := `UPDATE articles SET summary_brief = $1, summary_detailed = $2 WHERE id = $3`
	_, err := r.db.Exec(query, summaryBrief, summaryDetailed, id)
	return err
}

func (r *ArticleRepository) UpdateContent(id int, content string) error {
	query := `UPDATE articles SET content = $1, refetch_attempts = 0 WHERE id = $2`
	_, err := r.db.Exec(query, content, id)
	return err
}

func (r *ArticleRepository) IncrementRefetchAttempts(id int) error {
	query := `UPDATE articles SET refetch_attempts = refetch_attempts + 1 WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

func (r *ArticleRepository) UpdatePublishedAtIfNull(feedID int, url string, publishedAt *time.Time) error {
	if publishedAt == nil {
		return nil
	}
	query := `UPDATE articles SET published_at = $1 WHERE feed_id = $2 AND url = $3 AND published_at IS NULL`
	_, err := r.db.Exec(query, publishedAt, feedID, url)
	return err
}

func (r *ArticleRepository) GetRecommended(limit int, userID int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		FROM articles a
		LEFT JOIN (
			SELECT article_id, SUM(
				CASE signal_type
					WHEN 'like' THEN 5.0 * signal_value
					WHEN 'dislike' THEN -10.0 * signal_value
					WHEN 'save' THEN 3.0 * signal_value
					WHEN 'read_duration' THEN signal_value / 60.0
					ELSE 1.0 * signal_value
				END
			) as score
			FROM user_preferences
			WHERE created_at > NOW() - INTERVAL '30 days'
			AND user_id = $2
			GROUP BY article_id
		) p ON a.id = p.article_id
		WHERE p.score IS NOT NULL AND p.score > 0
		ORDER BY p.score DESC, a.published_at DESC NULLS LAST
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanArticleNoFeedTitle(rows)
}

func (r *ArticleRepository) GetArticlesForTopicExtraction(limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		FROM articles a
		JOIN user_preferences p ON a.id = p.article_id
		WHERE p.signal_type IN ('like', 'save')
		AND a.content IS NOT NULL AND a.content != ''
		ORDER BY p.created_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanArticleNoFeedTitle(rows)
}

func (r *ArticleRepository) GetArticlesWithoutSummary(limit int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at
		FROM articles
		WHERE (summary_brief IS NULL OR summary_brief = '')
		AND LENGTH(content) > 100
		ORDER BY fetched_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

func (r *ArticleRepository) GetArticlesWithShortContent(minLength int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at
		FROM articles
		WHERE url != '' AND refetch_attempts < 5
		  AND ((LENGTH(content) < $1 OR content IS NULL AND fetched_at > NOW() - INTERVAL '7 days')
		       OR (content LIKE '%<%>%' AND fetched_at > NOW() - INTERVAL '30 days'))
		ORDER BY fetched_at DESC
		LIMIT 50
	`
	rows, err := r.db.Query(query, minLength)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanArticleNoFeedTitle(rows)
}

func (r *ArticleRepository) GetUnreadCount(userID int) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		WHERE (f.owner_id IS NULL OR f.owner_id = $1)
		AND COALESCE(rp.is_completed, false) = false
	`
	var count int
	err := r.db.QueryRow(query, userID).Scan(&count)
	return count, err
}

func (r *ArticleRepository) Search(query string, userID, limit int) ([]model.Article, error) {
	q := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"
	sqlStr := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, f.title as feed_title,
		       COALESCE(rp.is_completed, false) as is_read
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $2
		WHERE (f.owner_id IS NULL OR f.owner_id = $2)
		  AND (a.title ILIKE $1 OR a.summary_brief ILIKE $1 OR a.content ILIKE $1)
		ORDER BY COALESCE(a.published_at, a.fetched_at) DESC
		LIMIT $3
	`
	rows, err := r.db.Query(sqlStr, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticle(rows)
}
