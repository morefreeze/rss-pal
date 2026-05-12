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
		var isLinkSet sql.NullBool
		var parentArticleID sql.NullInt64
		var processingState, editorNote sql.NullString
		var prerankScore sql.NullFloat64
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle, &isRead, &isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		a.IsLinkSet = isLinkSet.Bool
		if parentArticleID.Valid {
			v := int(parentArticleID.Int64)
			a.ParentArticleID = &v
		}
		a.ProcessingState = processingState.String
		if prerankScore.Valid {
			v := prerankScore.Float64
			a.PrerankScore = &v
		}
		a.EditorNote = editorNote.String
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
		var isLinkSet sql.NullBool
		var parentArticleID sql.NullInt64
		var processingState, editorNote sql.NullString
		var prerankScore sql.NullFloat64
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.IsLinkSet = isLinkSet.Bool
		if parentArticleID.Valid {
			v := int(parentArticleID.Int64)
			a.ParentArticleID = &v
		}
		a.ProcessingState = processingState.String
		if prerankScore.Valid {
			v := prerankScore.Float64
			a.PrerankScore = &v
		}
		a.EditorNote = editorNote.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		articles = append(articles, a)
	}
	return articles, nil
}

func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int) ([]model.Article, error) {
	query := `SELECT articles.id, articles.feed_id, articles.title, articles.url, articles.content, articles.published_at, articles.summary_brief, articles.summary_detailed, articles.fetched_at, articles.word_count, articles.reading_minutes, articles.media_url, articles.media_type, articles.media_duration_seconds, feeds.title as feed_title, COALESCE(rp.is_completed, false) as is_read, articles.is_link_set, articles.parent_article_id, articles.processing_state, articles.prerank_score, articles.editor_note
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

	// Sort by GREATEST(published_at, fetched_at): typical articles
	// (published ≤ fetched) keep chronological feel, but backfilled articles
	// from newly-added feeds (old published_at, recent fetched_at) bubble up
	// briefly so the new subscription is visible on /articles page 1.
	query += fmt.Sprintf(" ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(articles.published_at, articles.fetched_at), articles.fetched_at - INTERVAL '7 days')) DESC, COALESCE(articles.published_at, articles.fetched_at) DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
	var mediaDuration sql.NullInt64
	var isLinkSet sql.NullBool
	var parentArticleID sql.NullInt64
	var processingState, editorNote sql.NullString
	var prerankScore sql.NullFloat64
	err := r.db.QueryRow(query, id, userID).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle, &isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote)
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	a.IsLinkSet = isLinkSet.Bool
	if parentArticleID.Valid {
		v := int(parentArticleID.Int64)
		a.ParentArticleID = &v
	}
	a.ProcessingState = processingState.String
	if prerankScore.Valid {
		v := prerankScore.Float64
		a.PrerankScore = &v
	}
	a.EditorNote = editorNote.String
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, f.feed_type, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, feedType, mediaURL, mediaType sql.NullString
	var mediaDuration sql.NullInt64
	var isLinkSet sql.NullBool
	var parentArticleID sql.NullInt64
	var processingState, editorNote sql.NullString
	var prerankScore sql.NullFloat64
	err := r.db.QueryRow(query, id, userID).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
		&mediaURL, &mediaType, &mediaDuration,
		&feedTitle, &feedType,
		&isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote,
	)
	if err != nil {
		return nil, "", err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	a.IsLinkSet = isLinkSet.Bool
	if parentArticleID.Valid {
		v := int(parentArticleID.Int64)
		a.ParentArticleID = &v
	}
	a.ProcessingState = processingState.String
	if prerankScore.Valid {
		v := prerankScore.Float64
		a.PrerankScore = &v
	}
	a.EditorNote = editorNote.String
	a.MediaURL = mediaURL.String
	a.MediaType = mediaType.String
	a.MediaDurationSeconds = int(mediaDuration.Int64)
	return &a, feedType.String, nil
}

func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds, is_link_set, parent_article_id, processing_state, prerank_score, editor_note) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, fetched_at`
	mediaURL := nullableString(article.MediaURL)
	mediaType := nullableString(article.MediaType)
	mediaDuration := nullableInt(article.MediaDurationSeconds)
	var parentArticleID sql.NullInt64
	if article.ParentArticleID != nil {
		parentArticleID = sql.NullInt64{Int64: int64(*article.ParentArticleID), Valid: true}
	}
	var prerankScore sql.NullFloat64
	if article.PrerankScore != nil {
		prerankScore = sql.NullFloat64{Float64: *article.PrerankScore, Valid: true}
	}
	state := article.ProcessingState
	if state == "" {
		state = "ready"
	}
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes, mediaURL, mediaType, mediaDuration, article.IsLinkSet, parentArticleID, state, prerankScore, article.EditorNote).Scan(&article.ID, &article.FetchedAt)
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.url = $2
		LIMIT 1
	`
	var a model.Article
	var content, summaryBrief, summaryDetailed sql.NullString
	var isLinkSet sql.NullBool
	var parentArticleID sql.NullInt64
	var processingState, editorNote sql.NullString
	var prerankScore sql.NullFloat64
	err := r.db.QueryRow(query, ownerID, exactURL).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt,
		&isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote,
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
	a.IsLinkSet = isLinkSet.Bool
	if parentArticleID.Valid {
		v := int(parentArticleID.Int64)
		a.ParentArticleID = &v
	}
	a.ProcessingState = processingState.String
	if prerankScore.Valid {
		v := prerankScore.Float64
		a.PrerankScore = &v
	}
	a.EditorNote = editorNote.String
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

// GetMediaArticlesWithoutTranscript returns up to limit video/audio
// articles that have not yet had a transcript fetch attempt.
func (r *ArticleRepository) GetMediaArticlesWithoutTranscript(limit int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds, is_link_set, parent_article_id, processing_state, prerank_score, editor_note
		FROM articles
		WHERE transcript_fetched_at IS NULL
		  AND media_type IS NOT NULL
		  AND (media_type LIKE 'video/%' OR media_type LIKE 'audio/%')
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

// UpdateContentAndResetSummary atomically updates content + recomputed
// metrics, clears any existing summary, and stamps transcript_fetched_at.
// Used when transcript fetching succeeds. Clearing the summary is what
// feeds the article back to backfillSummaries on the next worker cycle.
func (r *ArticleRepository) UpdateContentAndResetSummary(id int, content string, wordCount, readingMinutes int) error {
	_, err := r.db.Exec(`
		UPDATE articles
		SET content = $1,
		    word_count = $2,
		    reading_minutes = $3,
		    summary_brief = NULL,
		    summary_detailed = NULL,
		    transcript_fetched_at = NOW(),
		    refetch_attempts = 0
		WHERE id = $4
	`, content, wordCount, readingMinutes, id)
	return err
}

// MarkTranscriptFetchAttempted records that we tried and failed to find
// a transcript for the article, preventing retries.
func (r *ArticleRepository) MarkTranscriptFetchAttempted(id int) error {
	_, err := r.db.Exec(`UPDATE articles SET transcript_fetched_at = NOW() WHERE id = $1`, id)
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		LEFT JOIN (
			SELECT article_id, SUM(
				CASE signal_type
					WHEN 'like' THEN 5.0 * signal_value
					WHEN 'dislike' THEN -10.0 * signal_value
					WHEN 'save' THEN 3.0 * signal_value
					WHEN 'read_duration' THEN signal_value / 60.0
					WHEN 'completed_listen' THEN 8.0 * signal_value
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN user_preferences p ON a.id = p.article_id
		WHERE p.signal_type IN ('like', 'save', 'completed_listen')
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
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds, is_link_set, parent_article_id, processing_state, prerank_score, editor_note
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.url != '' AND a.refetch_attempts < 5
		  AND f.feed_type NOT IN ('youtube', 'podcast')
		  AND (a.media_type IS NULL OR (a.media_type NOT LIKE 'video/%' AND a.media_type NOT LIKE 'audio/%'))
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
		       COALESCE(rp.is_completed, false) as is_read, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $2
		WHERE (f.owner_id IS NULL OR f.owner_id = $2)
		  AND (a.title ILIKE $1 OR a.summary_brief ILIKE $1 OR a.content ILIKE $1)
		ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(a.published_at, a.fetched_at), a.fetched_at - INTERVAL '7 days')) DESC,
		         COALESCE(a.published_at, a.fetched_at) DESC
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
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
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
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
					WHEN 'completed_listen' THEN 8.0 * signal_value
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

// GroupedTopN and GroupedPerGroupCap bound the /api/articles/grouped
// response server-side; the frontend renders 5 per group with an
// "展开更多" affordance up to PerGroupCap.
const (
	GroupedTopN        = 8
	GroupedPerGroupCap = 20
)

// GetGroupedByCategory returns the per-category view of articles visible to
// userID under the given filters. Top-N category groups are ranked by
// interest_categories.weight (falling back to article count for cold-start
// users); within each group articles are ranked by the same preference
// score formula that GetTopArticlesInRange uses. Unclassified articles
// (category IS NULL OR '') always come back in their own bucket. Category
// groups that don't make the top-N are not returned.
//
// The response JSON keeps the "topic" key for backward compat with the v1
// frontend; the value is now a category enum slug (e.g. "ai_eng") rather
// than a 2-4 字 中文 noun — the frontend label map renders it.
func (r *ArticleRepository) GetGroupedByCategory(userID int, feedID *int, unreadOnly, savedOnly bool) (*model.GroupedArticles, error) {
	// The same WHERE-clause shape used by GetAll, expressed as a string
	// fragment plus positional args we'll re-thread into each of the two
	// queries below. $1 is always userID.
	args := []interface{}{userID}
	conditions := []string{"(f.owner_id IS NULL OR f.owner_id = $1)"}
	joins := ""
	argIdx := 2
	if feedID != nil {
		conditions = append(conditions, fmt.Sprintf("a.feed_id = $%d", argIdx))
		args = append(args, *feedID)
		argIdx++
	}
	if unreadOnly {
		conditions = append(conditions, "COALESCE(rp.is_completed, false) = false")
	}
	if savedOnly {
		joins += " LEFT JOIN user_preferences up_save ON up_save.article_id = a.id AND up_save.user_id = $1 AND up_save.signal_type = 'save'"
		conditions = append(conditions, fmt.Sprintf("up_save.signal_value = $%d", argIdx))
		args = append(args, 1.0)
		argIdx++
	}
	where := strings.Join(conditions, " AND ")

	visibleCTE := `
WITH visible AS (
    SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
           a.summary_brief, a.summary_detailed, a.fetched_at,
           a.word_count, a.reading_minutes,
           a.media_url, a.media_type, a.media_duration_seconds,
           a.category,
           f.title AS feed_title,
           COALESCE(rp.is_completed, false) AS is_read,
           a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
    FROM articles a
    JOIN feeds f ON a.feed_id = f.id
    LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1` + joins + `
    WHERE ` + where + `
),
score AS (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'dislike' THEN -10.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            WHEN 'read_duration' THEN signal_value / 60.0
            WHEN 'completed_listen' THEN 8.0 * signal_value
            ELSE 1.0 * signal_value
        END
    ) AS s
    FROM user_preferences
    WHERE user_id = $1
    GROUP BY article_id
)`

	// --- Query 1: top-N category groups + up to PerGroupCap articles each ---
	groupsQuery := visibleCTE + fmt.Sprintf(`,
classified AS (
    SELECT * FROM visible WHERE category IS NOT NULL AND category <> ''
),
cat_stats AS (
    SELECT c.category,
           COUNT(*)::int AS article_count,
           COALESCE(MAX(ic.weight), 0) AS weight
    FROM classified c
    LEFT JOIN interest_categories ic ON ic.user_id = $1 AND ic.category = c.category
    GROUP BY c.category
    ORDER BY weight DESC, article_count DESC, c.category ASC
    LIMIT %d
),
ranked AS (
    SELECT c.*, COALESCE(s.s, 0) AS score,
           ROW_NUMBER() OVER (PARTITION BY c.category
                              ORDER BY COALESCE(s.s, 0) DESC, c.published_at DESC NULLS LAST, c.id DESC) AS rn
    FROM classified c
    LEFT JOIN score s ON c.id = s.article_id
    WHERE c.category IN (SELECT category FROM cat_stats)
)
SELECT cs.category, cs.article_count, cs.weight,
       r.id, r.feed_id, r.title, r.url, r.content, r.published_at,
       r.summary_brief, r.summary_detailed, r.fetched_at,
       r.word_count, r.reading_minutes,
       r.media_url, r.media_type, r.media_duration_seconds,
       r.feed_title, r.is_read,
       r.is_link_set, r.parent_article_id, r.processing_state, r.prerank_score, r.editor_note,
       r.rn
FROM cat_stats cs
JOIN ranked r ON r.category = cs.category AND r.rn <= %d
ORDER BY cs.weight DESC, cs.article_count DESC, cs.category ASC, r.rn ASC
`, GroupedTopN, GroupedPerGroupCap)

	groups, err := r.scanTopicGroups(groupsQuery, args)
	if err != nil {
		return nil, err
	}

	// --- Query 2a: unclassified bucket size ---
	unclassifiedCountQuery := visibleCTE + `
SELECT COUNT(*)::int FROM visible WHERE category IS NULL OR category = ''`
	var unclassifiedTotal int
	if err := r.db.QueryRow(unclassifiedCountQuery, args...).Scan(&unclassifiedTotal); err != nil {
		return nil, err
	}

	// --- Query 2b: top-PerGroupCap unclassified articles ---
	unclassifiedArticlesQuery := visibleCTE + fmt.Sprintf(`,
unclassified AS (
    SELECT * FROM visible WHERE category IS NULL OR category = ''
)
SELECT u.id, u.feed_id, u.title, u.url, u.content, u.published_at,
       u.summary_brief, u.summary_detailed, u.fetched_at,
       u.word_count, u.reading_minutes,
       u.media_url, u.media_type, u.media_duration_seconds,
       u.feed_title, u.is_read,
       u.is_link_set, u.parent_article_id, u.processing_state, u.prerank_score, u.editor_note
FROM unclassified u
LEFT JOIN score s ON u.id = s.article_id
ORDER BY COALESCE(s.s, 0) DESC, u.published_at DESC NULLS LAST, u.id DESC
LIMIT %d`, GroupedPerGroupCap)

	unclassified, err := r.scanFlatGroup(unclassifiedArticlesQuery, args, unclassifiedTotal)
	if err != nil {
		return nil, err
	}

	return &model.GroupedArticles{
		Groups:       groups,
		Unclassified: unclassified,
	}, nil
}

// scanTopicGroups consumes rows shaped by the groupsQuery: each row is
// (topic, article_count, weight, article fields, rn). Rows for the same
// topic arrive contiguously thanks to the outer ORDER BY, so we group
// linearly while preserving the SQL-side ordering.
func (r *ArticleRepository) scanTopicGroups(query string, args []interface{}) ([]model.TopicGroup, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []model.TopicGroup
	var currentTopic string
	var currentIdx = -1
	for rows.Next() {
		var topic string
		var articleCount int
		var weight float64
		var rn int
		var a model.Article
		var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		var isRead sql.NullBool
		var isLinkSet sql.NullBool
		var parentArticleID sql.NullInt64
		var processingState, editorNote sql.NullString
		var prerankScore sql.NullFloat64
		if err := rows.Scan(
			&topic, &articleCount, &weight,
			&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
			&summaryBrief, &summaryDetailed, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes,
			&mediaURL, &mediaType, &mediaDuration,
			&feedTitle, &isRead,
			&isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote,
			&rn,
		); err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		a.IsLinkSet = isLinkSet.Bool
		if parentArticleID.Valid {
			v := int(parentArticleID.Int64)
			a.ParentArticleID = &v
		}
		a.ProcessingState = processingState.String
		if prerankScore.Valid {
			v := prerankScore.Float64
			a.PrerankScore = &v
		}
		a.EditorNote = editorNote.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)

		if topic != currentTopic || currentIdx < 0 {
			groups = append(groups, model.TopicGroup{
				Topic:      topic,
				TotalCount: articleCount,
				Articles:   []model.Article{},
			})
			currentTopic = topic
			currentIdx = len(groups) - 1
		}
		groups[currentIdx].Articles = append(groups[currentIdx].Articles, a)
	}
	return groups, rows.Err()
}

// scanFlatGroup consumes rows shaped like (article fields) — no topic
// column, no rn. Used for the unclassified bucket. totalCount is plumbed
// in from a separate COUNT(*) query.
func (r *ArticleRepository) scanFlatGroup(query string, args []interface{}, totalCount int) (model.TopicGroup, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return model.TopicGroup{}, err
	}
	defer rows.Close()

	group := model.TopicGroup{Topic: "", TotalCount: totalCount, Articles: []model.Article{}}
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		var isRead sql.NullBool
		var isLinkSet sql.NullBool
		var parentArticleID sql.NullInt64
		var processingState, editorNote sql.NullString
		var prerankScore sql.NullFloat64
		if err := rows.Scan(
			&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
			&summaryBrief, &summaryDetailed, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes,
			&mediaURL, &mediaType, &mediaDuration,
			&feedTitle, &isRead,
			&isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote,
		); err != nil {
			return model.TopicGroup{}, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		a.IsLinkSet = isLinkSet.Bool
		if parentArticleID.Valid {
			v := int(parentArticleID.Int64)
			a.ParentArticleID = &v
		}
		a.ProcessingState = processingState.String
		if prerankScore.Valid {
			v := prerankScore.Float64
			a.PrerankScore = &v
		}
		a.EditorNote = editorNote.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		group.Articles = append(group.Articles, a)
	}
	return group, rows.Err()
}

// FindArticlesNeedingClassification returns up to `limit` articles without a
// cached category. Previously gated on "user signals within 7 days" to save
// tokens, but the 分组 view needs full coverage so that gate was dropped —
// the per-pass LIMIT and most-recent-first ordering still throttle cost.
// Articles missing only `topic` (but already classified for category) fall
// through here and get a second pass from the same prompt, which is fine.
func (r *ArticleRepository) FindArticlesNeedingClassification(limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		WHERE a.category IS NULL
		  AND a.content IS NOT NULL AND a.content <> ''
		ORDER BY a.fetched_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// SetClassification writes topic + tags + category onto an article. Pass
// empty strings / empty slice to mark the article as "AI returned nothing"
// for that field (still cached at the row level via the column being
// non-NULL... see SetCategory). Use nullableString so an empty category
// stays NULL — both the worker pass and the grouping query treat NULL as
// "not yet classified" and re-attempt / hide-in-unclassified accordingly.
func (r *ArticleRepository) SetClassification(articleID int, topic string, tags []string, category string) error {
	_, err := r.db.Exec(
		`UPDATE articles SET topic = $1, tags = $2, category = $3 WHERE id = $4`,
		topic, pq.Array(tags), nullableString(category), articleID,
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


// FindParentsNeedingExpansion returns parent articles (is_link_set=true) that
// have zero rows in articles with parent_article_id = parent.id. Limit caps
// per-cycle work.
func (r *ArticleRepository) FindParentsNeedingExpansion(limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
		       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count,
		       a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds,
		       a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		WHERE a.is_link_set = true
		  AND NOT EXISTS (SELECT 1 FROM articles c WHERE c.parent_article_id = a.id)
		ORDER BY a.fetched_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// GetChildren returns children of a parent ordered by prerank_score DESC, id ASC.
// scanArticleNoFeedTitle already populates all link_set fields, so no supplemental
// query is needed.
func (r *ArticleRepository) GetChildren(parentID int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at,
		       summary_brief, summary_detailed, fetched_at, word_count,
		       reading_minutes, media_url, media_type, media_duration_seconds,
		       is_link_set, parent_article_id, processing_state, prerank_score, editor_note
		FROM articles
		WHERE parent_article_id = $1
		ORDER BY prerank_score DESC NULLS LAST, id ASC
	`
	rows, err := r.db.Query(query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// UpdateProcessingState transitions an article's processing_state.
// Returns rowsAffected so callers can detect "already in target state".
func (r *ArticleRepository) UpdateProcessingState(id int, from, to string) (int64, error) {
	res, err := r.db.Exec(`UPDATE articles SET processing_state = $1 WHERE id = $2 AND processing_state = $3`, to, id, from)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetProcessingChildren returns children with processing_state='processing'
// and refetch_attempts < 3, capped at limit.
func (r *ArticleRepository) GetProcessingChildren(limit int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at,
		       summary_brief, summary_detailed, fetched_at, word_count, reading_minutes,
		       media_url, media_type, media_duration_seconds,
		       is_link_set, parent_article_id, processing_state, prerank_score, editor_note
		FROM articles
		WHERE processing_state = 'processing'
		  AND parent_article_id IS NOT NULL
		  AND COALESCE(refetch_attempts, 0) < 3
		ORDER BY prerank_score DESC NULLS LAST, id ASC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// MarkFailed transitions an article unconditionally to 'failed'.
func (r *ArticleRepository) MarkFailed(id int) error {
	_, err := r.db.Exec(`UPDATE articles SET processing_state = 'failed' WHERE id = $1`, id)
	return err
}

// MarkFailedAfterRetries transitions processing_state to 'failed' if and only
// if refetch_attempts has reached threshold. Safe to call after every failure.
func (r *ArticleRepository) MarkFailedAfterRetries(id, threshold int) error {
	_, err := r.db.Exec(`
		UPDATE articles
		SET processing_state = 'failed'
		WHERE id = $1 AND COALESCE(refetch_attempts, 0) >= $2
	`, id, threshold)
	return err
}

// GetLinkSetRecommendations returns processed children from link_set parents
// fetched in the last `days` days, scored by the same formula as GetRecommended.
// Filters to processing_state='ready' and visible-to-user feeds.
func (r *ArticleRepository) GetLinkSetRecommendations(userID, days, limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
		       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count,
		       a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds,
		       a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN articles parent ON a.parent_article_id = parent.id
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN (
			SELECT article_id, SUM(
				CASE signal_type
					WHEN 'like' THEN 5.0 * signal_value
					WHEN 'dislike' THEN -10.0 * signal_value
					WHEN 'save' THEN 3.0 * signal_value
					WHEN 'read_duration' THEN signal_value / 60.0
					WHEN 'completed_listen' THEN 8.0 * signal_value
					ELSE 1.0 * signal_value
				END
			) AS pref_score
			FROM user_preferences
			WHERE created_at > NOW() - INTERVAL '30 days' AND user_id = $1
			GROUP BY article_id
		) p ON a.id = p.article_id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		WHERE a.processing_state = 'ready'
		  AND a.parent_article_id IS NOT NULL
		  AND parent.fetched_at > NOW() - ($2 || ' days')::INTERVAL
		  AND (f.owner_id IS NULL OR f.owner_id = $1)
		  AND COALESCE(rp.is_completed, false) = false
		ORDER BY COALESCE(p.pref_score, 0) + COALESCE(a.prerank_score, 0) DESC,
		         a.published_at DESC NULLS LAST
		LIMIT $3
	`
	rows, err := r.db.Query(query, userID, days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
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
       f.title AS feed_title,
       a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
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
            WHEN 'completed_listen' THEN 8.0 * signal_value
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
       f.title AS feed_title,
       a.is_link_set, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
FROM articles a
JOIN feeds f ON a.feed_id = f.id
JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
JOIN (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            WHEN 'completed_listen' THEN 2.0 * signal_value
            ELSE 0
        END
    ) AS score
    FROM user_preferences
    WHERE user_id = $1 AND signal_type IN ('like','save','completed_listen')
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
			var isLinkSet sql.NullBool
			var parentArticleID sql.NullInt64
			var processingState, editorNote sql.NullString
			var prerankScore sql.NullFloat64
			if err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
				&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
				&mediaURL, &mediaType, &mediaDuration,
				&feedTitle,
				&isLinkSet, &parentArticleID, &processingState, &prerankScore, &editorNote); err != nil {
				return err
			}
			a.Content = content.String
			a.SummaryBrief = summaryBrief.String
			a.SummaryDetailed = summaryDetailed.String
			a.FeedTitle = feedTitle.String
			a.IsLinkSet = isLinkSet.Bool
			if parentArticleID.Valid {
				v := int(parentArticleID.Int64)
				a.ParentArticleID = &v
			}
			a.ProcessingState = processingState.String
			if prerankScore.Valid {
				v := prerankScore.Float64
				a.PrerankScore = &v
			}
			a.EditorNote = editorNote.String
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

