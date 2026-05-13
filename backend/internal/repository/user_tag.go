package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

// TagSidebarData is the response shape for GET /api/tags/sidebar.
type TagSidebarData struct {
	Tags          []model.UserTag `json:"tags"`           // article_count populated
	TotalCount    int             `json:"total_count"`    // articles under the filter (no tag scoping)
	UntaggedCount int             `json:"untagged_count"` // articles with zero manual tags
}

// EffectiveSource is the user-facing source identifier for an article.
// For bookmarklet (feed_type='saved') articles, it derives from URL host;
// for normal feeds, it's the feed itself.
type EffectiveSource struct {
	Key   string `json:"key"`   // "feed:<id>" or "host:<host>"
	Title string `json:"title"`
}

// extractHost returns the URL's host stripped of "www." prefix.
// Returns empty string if URL is unparsable.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// effectiveSourceFor builds an EffectiveSource from a feed and article URL.
// Saved-feed articles surface their host; everything else falls back to
// the feed's own (id, title) pair so the chip in the UI stays stable.
func effectiveSourceFor(feedID int, feedTitle, feedType, articleURL string) EffectiveSource {
	if feedType == "saved" {
		if host := extractHost(articleURL); host != "" {
			return EffectiveSource{Key: "host:" + host, Title: host}
		}
	}
	return EffectiveSource{
		Key:   fmt.Sprintf("feed:%d", feedID),
		Title: feedTitle,
	}
}

// ErrTagNameConflict is returned when a tag name already exists for the user.
var ErrTagNameConflict = errors.New("tag name already exists")

type UserTagRepository struct {
	db *sql.DB
}

func NewUserTagRepository(db *sql.DB) *UserTagRepository {
	return &UserTagRepository{db: db}
}

// GetTagsForUser returns the user's manual tags with the count of distinct
// SAVED articles each tag is currently bound to. Tags with zero saved
// articles are still returned (they may be bound to non-saved articles).
func (r *UserTagRepository) GetTagsForUser(userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at,
		       COUNT(DISTINCT CASE WHEN p.article_id IS NOT NULL THEN aut.article_id END) AS article_count
		FROM user_tags t
		LEFT JOIN article_user_tags aut
		       ON aut.tag_id = t.id AND aut.user_id = t.user_id
		LEFT JOIN user_preferences p
		       ON p.article_id = aut.article_id
		      AND p.user_id = t.user_id
		      AND p.signal_type = 'save'
		WHERE t.user_id = $1
		GROUP BY t.id
		ORDER BY t.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ArticleCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// GetTagsForSidebar returns tags with dynamic counts under the article
// filter, plus the matching total and untagged counts. Filter shape
// matches what /api/articles accepts (without TagID/Untagged — those
// would only filter on themselves).
func (r *UserTagRepository) GetTagsForSidebar(filter ArticleFilter) (*TagSidebarData, error) {
	// Tags + per-tag count
	// $1 is reserved for t.user_id; filter args start at $2
	joins, whereFrags, args, _ := buildArticleFilterSQL(filter, "a", 2)
	tagsQuery := `
        SELECT t.id, t.user_id, t.name, t.created_at,
               COUNT(DISTINCT aut.article_id) AS article_count
        FROM user_tags t
        JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
        JOIN articles a ON a.id = aut.article_id` + joins + `
        WHERE t.user_id = $1`
	for _, w := range whereFrags {
		tagsQuery += " AND " + w
	}
	tagsQuery += `
        GROUP BY t.id, t.user_id, t.name, t.created_at
        HAVING COUNT(DISTINCT aut.article_id) > 0
        ORDER BY t.name ASC`
	qargs := append([]any{filter.UserID}, args...)
	rows, err := r.db.Query(tagsQuery, qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []model.UserTag{}
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ArticleCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// total + untagged counts — same filter, plus the feeds visibility
	// guard that GetAll applies (so counts agree with what the list returns).
	joins2, where2, args2, _ := buildArticleFilterSQL(filter, "articles", 1)
	visibilityFrag := "(feeds.owner_id IS NULL OR feeds.owner_id = $1)"
	totalQuery := `SELECT COUNT(*) FROM articles JOIN feeds ON articles.feed_id = feeds.id` + joins2
	untaggedFrag := fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM article_user_tags aut WHERE aut.article_id = articles.id AND aut.user_id = $%d)`,
		len(args2)+1)
	untaggedArgs := append([]any{}, args2...)
	untaggedArgs = append(untaggedArgs, filter.UserID)
	untaggedQuery := `SELECT COUNT(*) FROM articles JOIN feeds ON articles.feed_id = feeds.id` + joins2

	clause := " WHERE " + visibilityFrag
	for _, w := range where2 {
		clause += " AND " + w
	}
	totalQuery += clause
	untaggedQuery += clause + " AND " + untaggedFrag

	var total, untagged int
	if err := r.db.QueryRow(totalQuery, args2...).Scan(&total); err != nil {
		return nil, err
	}
	if err := r.db.QueryRow(untaggedQuery, untaggedArgs...).Scan(&untagged); err != nil {
		return nil, err
	}

	return &TagSidebarData{
		Tags:          tags,
		TotalCount:    total,
		UntaggedCount: untagged,
	}, nil
}

// CreateTag inserts (or returns existing) a tag by (user_id, name).
// Returns the existing or newly-created tag's ID.
func (r *UserTagRepository) CreateTag(userID int, name string) (int, error) {
	var id int
	err := r.db.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&id)
	return id, err
}

// RenameTag changes the name. Returns ErrTagNameConflict on unique violation.
// Returns sql.ErrNoRows if the tag does not belong to the user.
func (r *UserTagRepository) RenameTag(userID, tagID int, name string) error {
	var id int
	err := r.db.QueryRow(`
		UPDATE user_tags SET name = $1 WHERE id = $2 AND user_id = $3 RETURNING id
	`, name, tagID, userID).Scan(&id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ErrTagNameConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	return nil
}

// DeleteTag removes a tag (cascades article_user_tags via FK).
// Returns sql.ErrNoRows if not found / not owned.
func (r *UserTagRepository) DeleteTag(userID, tagID int) error {
	res, err := r.db.Exec(`DELETE FROM user_tags WHERE id = $1 AND user_id = $2`, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type ArticleUserTagRepository struct {
	db *sql.DB
}

func NewArticleUserTagRepository(db *sql.DB) *ArticleUserTagRepository {
	return &ArticleUserTagRepository{db: db}
}

// BindByName ensures (article_id, tag with given name, user) is bound.
// Creates the tag in the user's dictionary if it does not exist.
// Idempotent: returns the tag ID whether new or pre-existing.
func (r *ArticleUserTagRepository) BindByName(articleID, userID int, name string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var tagID int
	err = tx.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&tagID)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`
		INSERT INTO article_user_tags (article_id, tag_id, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (article_id, tag_id) DO NOTHING
	`, articleID, tagID, userID)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return tagID, nil
}

// Unbind removes the binding. Returns sql.ErrNoRows if not bound.
func (r *ArticleUserTagRepository) Unbind(articleID, tagID, userID int) error {
	res, err := r.db.Exec(`
		DELETE FROM article_user_tags
		WHERE article_id = $1 AND tag_id = $2 AND user_id = $3
	`, articleID, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSourceForArticle returns the feed-derived source tag (id + title).
// Enforces feed ownership: returns sql.ErrNoRows if the article belongs to
// a feed owned by another user.
//
// For bookmarklet (feed_type='saved') articles the human-readable title is
// the article URL's host, not the bin's own "⭐ 网摘". The FeedID stays the
// real feeds.id so any caller mapping back to a feed still works.
func (r *ArticleUserTagRepository) GetSourceForArticle(articleID, userID int) (model.ArticleTagSource, error) {
	var s model.ArticleTagSource
	var feedTitle, feedType, articleURL string
	var feedID int
	err := r.db.QueryRow(`
		SELECT f.id, f.title, COALESCE(f.feed_type, 'rss'), a.url
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)
	`, articleID, userID).Scan(&feedID, &feedTitle, &feedType, &articleURL)
	if err != nil {
		return s, err
	}
	s.FeedID = feedID
	if feedType == "saved" {
		if host := extractHost(articleURL); host != "" {
			s.Title = host
			return s, nil
		}
	}
	s.Title = feedTitle
	return s, nil
}

// GetManualForArticle returns the user's manual tags bound to the article.
func (r *ArticleUserTagRepository) GetManualForArticle(articleID, userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = $1 AND t.user_id = $2
		ORDER BY t.name ASC
	`, articleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// GetManualForArticles batch version — returns map[articleID][]UserTag.
// Used by /api/saved to attach tags to article cards.
func (r *ArticleUserTagRepository) GetManualForArticles(articleIDs []int, userID int) (map[int][]model.UserTag, error) {
	out := map[int][]model.UserTag{}
	if len(articleIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(`
		SELECT aut.article_id, t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = ANY($1::int[]) AND t.user_id = $2
		ORDER BY aut.article_id, t.name ASC
	`, pq.Array(articleIDs), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var aid int
		var t model.UserTag
		if err := rows.Scan(&aid, &t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out[aid] = append(out[aid], t)
	}
	return out, rows.Err()
}

type SavedRepository struct {
	db *sql.DB
}

func NewSavedRepository(db *sql.DB) *SavedRepository {
	return &SavedRepository{db: db}
}

// SavedQuery describes a /api/saved request.
//
// SourceKind / SourceValue replace the old SourceFeedID. Two modes:
//   - kind="feed", value="<id>"   → filter a.feed_id = id
//   - kind="host", value="<host>" → filter on host extracted from a.url
//     (lower-cased, "www." stripped) — used to drill into a single
//     bookmarklet source.
type SavedQuery struct {
	UserID      int
	TagIDs      []int  // empty = "all"
	Mode        string // "and" | "or"; only honored when len(TagIDs)>1
	Untagged    bool   // overrides TagIDs when true
	SourceKind  string // "" | "feed" | "host"
	SourceValue string // feed-id-as-string for "feed", host string for "host"
	Limit       int
	Offset      int
}

// SavedRow pairs an Article with the EffectiveSource the UI should render.
// Centralising this here lets the handler build the response without an
// extra DB round-trip per item.
type SavedRow struct {
	Article         model.Article
	EffectiveSource EffectiveSource
}

// ListSaved returns one SavedRow per saved article (in published order) and a total count.
func (r *SavedRepository) ListSaved(q SavedQuery) ([]SavedRow, int, error) {
	args := []interface{}{q.UserID}
	// "In my collection" = either I save-signaled it, or it's in my bookmarklet
	// (网摘) feed. Both branches are user-scoped via $1.
	where := []string{`(
		EXISTS (
			SELECT 1 FROM user_preferences p
			WHERE p.article_id = a.id
			  AND p.user_id = $1
			  AND p.signal_type = 'save'
		)
		OR (f.feed_type = 'saved' AND f.owner_id = $1)
	)`}
	// Tenancy: never expose articles from feeds owned by another user.
	// Mirrors the pattern used by every other articles query in this codebase.
	where = append(where, `(f.owner_id IS NULL OR f.owner_id = $1)`)

	if q.Untagged {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM article_user_tags aut
			WHERE aut.article_id = a.id AND aut.user_id = $1
		)`)
	} else if len(q.TagIDs) > 0 {
		args = append(args, pq.Array(q.TagIDs))
		idsParam := "$" + strconv.Itoa(len(args))
		if q.Mode == "and" && len(q.TagIDs) > 1 {
			args = append(args, len(q.TagIDs))
			countParam := "$" + strconv.Itoa(len(args))
			where = append(where, `(
				SELECT COUNT(DISTINCT aut.tag_id) FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			) = `+countParam)
		} else {
			where = append(where, `EXISTS (
				SELECT 1 FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			)`)
		}
	}

	switch q.SourceKind {
	case "feed":
		if q.SourceValue != "" {
			args = append(args, q.SourceValue)
			where = append(where, `a.feed_id::text = $`+strconv.Itoa(len(args)))
		}
	case "host":
		if q.SourceValue != "" {
			args = append(args, q.SourceValue)
			// Extract host from a.url in SQL: strip optional scheme + "www." prefix,
			// take everything up to the next "/", and lowercase. Mirrors extractHost.
			where = append(where, `lower(regexp_replace(a.url, '^https?://(?:www\.)?([^/]+).*$', '\1')) = lower($`+strconv.Itoa(len(args))+`)`)
		}
	}

	whereSQL := strings.Join(where, " AND ")

	// Count
	var total int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Page — also fetch f.feed_type so we can build effective_source per row
	// without an extra round-trip.
	args = append(args, q.Limit, q.Offset)
	limitParam := "$" + strconv.Itoa(len(args)-1)
	offsetParam := "$" + strconv.Itoa(len(args))
	rows, err := r.db.Query(`
		SELECT a.id, a.feed_id, f.title AS feed_title, COALESCE(f.feed_type, 'rss') AS feed_type,
		       a.title, a.url,
		       a.published_at, a.summary_brief, a.fetched_at,
		       COALESCE(a.word_count, 0), COALESCE(a.reading_minutes, 0),
		       COALESCE(a.media_type, '')
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE `+whereSQL+`
		ORDER BY a.published_at DESC NULLS LAST, a.id DESC
		LIMIT `+limitParam+` OFFSET `+offsetParam, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []SavedRow
	for rows.Next() {
		var a model.Article
		var summary, mediaType sql.NullString
		var feedTitle sql.NullString
		var feedType string
		if err := rows.Scan(
			&a.ID, &a.FeedID, &feedTitle, &feedType, &a.Title, &a.URL,
			&a.PublishedAt, &summary, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes, &mediaType,
		); err != nil {
			return nil, 0, err
		}
		a.FeedTitle = feedTitle.String
		a.SummaryBrief = summary.String
		a.MediaType = mediaType.String
		out = append(out, SavedRow{
			Article:         a,
			EffectiveSource: effectiveSourceFor(a.FeedID, feedTitle.String, feedType, a.URL),
		})
	}
	return out, total, rows.Err()
}

type TagSuggestionRepository struct {
	db *sql.DB
}

func NewTagSuggestionRepository(db *sql.DB) *TagSuggestionRepository {
	return &TagSuggestionRepository{db: db}
}

// SuggestionsForArticle returns up to 5 candidate names from articles.tags,
// filtered to remove tags the user has already adopted (in user_tags + bound)
// or dismissed. Returns empty slice if articles.tags is null/empty.
//
// Tenancy guard: the inner SELECT joins feeds and requires the article to
// belong to a feed owned by the user (or owner_id IS NULL for legacy/global
// feeds). If the article is not visible to the user, the inner SELECT yields
// no rows and unnest(COALESCE(..., ARRAY[]::TEXT[])) yields zero rows, so
// the function returns []. This prevents probing other users' articles.
func (r *TagSuggestionRepository) SuggestionsForArticle(articleID, userID int) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT t AS name
		FROM unnest(COALESCE(
			(SELECT a.tags FROM articles a
			 JOIN feeds f ON f.id = a.feed_id
			 WHERE a.id = $2 AND (f.owner_id IS NULL OR f.owner_id = $1)),
			ARRAY[]::TEXT[]
		)) AS t
		WHERE NOT EXISTS (
			SELECT 1 FROM tag_suggestion_dismissals d
			WHERE d.article_id = $2 AND d.user_id = $1 AND d.name = t
		)
		AND NOT EXISTS (
			SELECT 1 FROM user_tags ut
			JOIN article_user_tags aut
			       ON aut.tag_id = ut.id AND aut.user_id = ut.user_id
			WHERE ut.user_id = $1 AND ut.name = t AND aut.article_id = $2
		)
		LIMIT 5
	`, userID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DismissSuggestion records (article_id, user_id, name) so the user does not
// see this candidate again. Idempotent. Dismissal is harmless (only affects
// this user's view of suggestions), so no separate ownership check is needed.
func (r *TagSuggestionRepository) DismissSuggestion(articleID, userID int, name string) error {
	_, err := r.db.Exec(`
		INSERT INTO tag_suggestion_dismissals (article_id, user_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (article_id, user_id, name) DO NOTHING
	`, articleID, userID, name)
	return err
}
