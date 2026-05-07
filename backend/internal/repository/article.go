package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
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
		var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		var isRead sql.NullBool
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle, &isRead)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		articles = append(articles, a)
	}
	return articles, nil
}

func (r *ArticleRepository) scanArticleNoFeedTitle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		articles = append(articles, a)
	}
	return articles, nil
}

func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int) ([]model.Article, error) {
	query := `SELECT articles.id, articles.feed_id, articles.title, articles.url, articles.content, articles.published_at, articles.summary_brief, articles.summary_detailed, articles.fetched_at, articles.word_count, articles.reading_minutes, articles.media_url, articles.media_type, articles.media_duration_seconds, feeds.title as feed_title, COALESCE(rp.is_completed, false) as is_read
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
	var mediaDuration sql.NullInt64
	err := r.db.QueryRow(query, id, userID).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle)
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	a.MediaURL = mediaURL.String
	a.MediaType = mediaType.String
	a.MediaDurationSeconds = int(mediaDuration.Int64)
	return &a, nil
}

// GetByIDWithFeedType returns the article alongside its feed's feed_type
// (e.g., "rss" / "saved" / "youtube"). Used by the article handler to derive
// the from_bookmarklet response field without modifying model.Article.
func (r *ArticleRepository) GetByIDWithFeedType(id, userID int) (*model.Article, string, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, f.feed_type
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, feedType, mediaURL, mediaType sql.NullString
	var mediaDuration sql.NullInt64
	err := r.db.QueryRow(query, id, userID).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
		&mediaURL, &mediaType, &mediaDuration,
		&feedTitle, &feedType,
	)
	if err != nil {
		return nil, "", err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	a.MediaURL = mediaURL.String
	a.MediaType = mediaType.String
	a.MediaDurationSeconds = int(mediaDuration.Int64)
	return &a, feedType.String, nil
}

func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, fetched_at`
	mediaURL := nullableString(article.MediaURL)
	mediaType := nullableString(article.MediaType)
	mediaDuration := nullableInt(article.MediaDurationSeconds)
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes, mediaURL, mediaType, mediaDuration).Scan(&article.ID, &article.FetchedAt)
}

// nullableString returns a sql.NullString that's NULL when s is empty.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullableInt returns a sql.NullInt64 that's NULL when n is zero.
func nullableInt(n int) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}

// UpdateMediaIfNull fills the three media columns for an existing row, but
// only when media_url is currently NULL. Idempotent: subsequent calls on a
// row that already has media are no-ops. Used by the worker to backfill
// historical podcast episodes the first time we see them after this feature
// ships, without overwriting hand-edited or richer data.
func (r *ArticleRepository) UpdateMediaIfNull(feedID int, url, mediaURL, mediaType string, durationSeconds int) error {
	if mediaURL == "" {
		return nil
	}
	_, err := r.db.Exec(`
		UPDATE articles
		SET media_url = $3, media_type = $4, media_duration_seconds = $5
		WHERE feed_id = $1 AND url = $2 AND media_url IS NULL
	`, feedID, url, mediaURL, nullableString(mediaType), nullableInt(durationSeconds))
	return err
}

func (r *ArticleRepository) Exists(feedID int, url string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM articles WHERE feed_id = $1 AND url = $2)`
	var exists bool
	err := r.db.QueryRow(query, feedID, url).Scan(&exists)
	return exists, err
}

// FindByOwnerAndURL returns the article matching exactURL within any feed
// visible to ownerID — that is, feeds the user owns OR shared admin feeds
// (owner_id IS NULL). Returns (nil, nil) if no match. Caller is responsible
// for passing a normalized URL (see util.NormalizeURL).
func (r *ArticleRepository) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.url = $2
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

func (r *ArticleRepository) UpdateContent(id int, content string, wordCount, readingMinutes int) error {
	_, err := r.db.Exec(`UPDATE articles SET content = $1, word_count = $2, reading_minutes = $3, refetch_attempts = 0 WHERE id = $4`, content, wordCount, readingMinutes, id)
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds
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
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $2
		WHERE p.score IS NOT NULL AND p.score > 0
		AND COALESCE(rp.is_completed, false) = false
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds
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
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.url != '' AND a.refetch_attempts < 5
		  AND f.feed_type NOT IN ('youtube', 'podcast')
		  AND ((LENGTH(a.content) < $1 OR a.content IS NULL AND a.fetched_at > NOW() - INTERVAL '7 days')
		       OR (a.content LIKE '%<%>%' AND a.fetched_at > NOW() - INTERVAL '30 days'))
		ORDER BY a.fetched_at DESC
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title,
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

// GetByIDsForUser fetches the given article IDs in the order they appear in
// `ids`. Used by the weekly digest to honor the "frozen snapshot" semantic:
// once a digest is generated for a week, the article set is locked.
func (r *ArticleRepository) GetByIDsForUser(userID int, ids []int) ([]model.Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	int64s := make(pq.Int64Array, len(ids))
	for i, id := range ids {
		int64s[i] = int64(id)
	}
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		WHERE a.id = ANY($2) AND (f.owner_id IS NULL OR f.owner_id = $1)
		ORDER BY array_position($2, a.id)
	`
	rows, err := r.db.Query(query, userID, int64s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticle(rows)
}

// GetTopArticlesInRange returns up to `limit` articles from feeds visible to
// `userID` whose published_at falls in [start, end). Ranks by personalization
// score (mirrors GetRecommended), tie-breaking by published_at desc. Falls
// back to recency for users with no preference signals.
func (r *ArticleRepository) GetTopArticlesInRange(userID int, start, end time.Time, limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
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
			WHERE user_id = $1
			GROUP BY article_id
		) p ON a.id = p.article_id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1)
		  AND a.published_at >= $2 AND a.published_at < $3
		ORDER BY COALESCE(p.score, 0) DESC, a.published_at DESC
		LIMIT $4
	`
	rows, err := r.db.Query(query, userID, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticle(rows)
}

// FindArticlesNeedingClassification returns up to `limit` articles that have
// strong signals in the last 7 days but no cached topic.
func (r *ArticleRepository) FindArticlesNeedingClassification(limit int) ([]model.Article, error) {
	query := `
		SELECT DISTINCT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds
		FROM articles a
		JOIN user_preferences up ON up.article_id = a.id
		WHERE a.topic IS NULL
		  AND up.created_at > NOW() - INTERVAL '7 days'
		  AND (
		    up.signal_type IN ('like','save')
		    OR (up.signal_type = 'read_duration' AND up.signal_value >= 60)
		  )
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// SetClassification writes topic + tags onto an article. Pass empty string and
// empty slice to mark the article as "AI returned nothing" (still cached, won't retry).
func (r *ArticleRepository) SetClassification(articleID int, topic string, tags []string) error {
	_, err := r.db.Exec(
		`UPDATE articles SET topic = $1, tags = $2 WHERE id = $3`,
		topic, pq.Array(tags), articleID,
	)
	return err
}

// GetClassification reads the cached topic + tags for one article.
// Returns ("", nil, nil) when not yet classified.
func (r *ArticleRepository) GetClassification(articleID int) (string, []string, error) {
	var topic sql.NullString
	var tags pq.StringArray
	err := r.db.QueryRow(
		`SELECT topic, tags FROM articles WHERE id = $1`, articleID,
	).Scan(&topic, &tags)
	if err != nil {
		return "", nil, err
	}
	return topic.String, []string(tags), nil
}

// GetTopTopicVocabulary returns the most-frequent topics across articles, used
// as a recommendation list for the AI classifier (B3 self-stabilizing vocabulary).
func (r *ArticleRepository) GetTopTopicVocabulary(limit int) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT topic
		FROM articles
		WHERE topic IS NOT NULL AND topic <> ''
		GROUP BY topic
		ORDER BY COUNT(*) DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}


// GetInsightCandidates returns up to (unreadLimit + readLimit) candidate
// articles for the AI prompt:
//
//   - unread block: visible to userID, not yet completed, ranked by score
//     (existing user_preferences signal weighting) DESC, then published_at
//     DESC NULLS LAST. Caps at unreadLimit. AlreadyRead=false.
//
//   - past-favorites block: visible to userID, is_completed=true, has at
//     least one 'like' or 'save' signal, last_read_at between 30 and 180
//     days ago. Same score+recency ranking. Caps at readLimit. AlreadyRead=true.
//
// The two blocks are concatenated (unread first). They are disjoint by
// is_completed, so no extra dedup is needed.
func (r *ArticleRepository) GetInsightCandidates(userID, unreadLimit, readLimit int) ([]model.InsightCandidate, error) {
	const unreadQuery = `
SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes,
       a.media_url, a.media_type, a.media_duration_seconds,
       f.title AS feed_title
FROM articles a
JOIN feeds f ON a.feed_id = f.id
LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
LEFT JOIN (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'dislike' THEN -10.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            WHEN 'read_duration' THEN signal_value / 60.0
            ELSE 1.0 * signal_value
        END
    ) AS score
    FROM user_preferences
    WHERE user_id = $1 AND created_at > NOW() - INTERVAL '30 days'
    GROUP BY article_id
) p ON a.id = p.article_id
WHERE (f.owner_id IS NULL OR f.owner_id = $1)
  AND COALESCE(rp.is_completed, false) = false
ORDER BY COALESCE(p.score, 0) DESC, a.published_at DESC NULLS LAST
LIMIT $2
`
	const readQuery = `
SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes,
       a.media_url, a.media_type, a.media_duration_seconds,
       f.title AS feed_title
FROM articles a
JOIN feeds f ON a.feed_id = f.id
JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
JOIN (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            ELSE 0
        END
    ) AS score
    FROM user_preferences
    WHERE user_id = $1 AND signal_type IN ('like','save')
    GROUP BY article_id
) p ON a.id = p.article_id
WHERE (f.owner_id IS NULL OR f.owner_id = $1)
  AND rp.is_completed = true
  AND rp.last_read_at BETWEEN NOW() - INTERVAL '180 days' AND NOW() - INTERVAL '30 days'
  AND p.score > 0
ORDER BY p.score DESC, rp.last_read_at DESC
LIMIT $2
`

	scan := func(rows *sql.Rows, alreadyRead bool, out *[]model.InsightCandidate) error {
		defer rows.Close()
		for rows.Next() {
			var a model.Article
			var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
			var mediaDuration sql.NullInt64
			if err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
				&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
				&mediaURL, &mediaType, &mediaDuration,
				&feedTitle); err != nil {
				return err
			}
			a.Content = content.String
			a.SummaryBrief = summaryBrief.String
			a.SummaryDetailed = summaryDetailed.String
			a.FeedTitle = feedTitle.String
			a.MediaURL = mediaURL.String
			a.MediaType = mediaType.String
			a.MediaDurationSeconds = int(mediaDuration.Int64)
			brief := []rune(a.SummaryBrief)
			if len(brief) > 60 {
				brief = brief[:60]
			}
			*out = append(*out, model.InsightCandidate{
				Article:     a,
				AlreadyRead: alreadyRead,
				BriefShort:  string(brief),
			})
		}
		return rows.Err()
	}

	var out []model.InsightCandidate
	if unreadLimit > 0 {
		rows, err := r.db.Query(unreadQuery, userID, unreadLimit)
		if err != nil {
			return nil, err
		}
		if err := scan(rows, false, &out); err != nil {
			return nil, err
		}
	}
	if readLimit > 0 {
		rows, err := r.db.Query(readQuery, userID, readLimit)
		if err != nil {
			return nil, err
		}
		if err := scan(rows, true, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

