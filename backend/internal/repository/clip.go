package repository

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

type ClipRepository struct {
	db *sql.DB
}

func NewClipRepository(db *sql.DB) *ClipRepository {
	return &ClipRepository{db: db}
}

// ClipQuery describes a /api/clip request.
//
// SourceKind / SourceValue:
//   - kind="feed", value="<id>"   → filter a.feed_id = id
//   - kind="host", value="<host>" → filter on host extracted from a.url
//     (lower-cased, "www." stripped) — used to drill into a single clip source.
type ClipQuery struct {
	UserID      int
	TagIDs      []int  // empty = "all"
	Mode        string // "and" | "or"; only honored when len(TagIDs)>1
	Untagged    bool   // overrides TagIDs when true
	SourceKind  string // "" | "feed" | "host"
	SourceValue string
	Limit       int
	Offset      int
}

// ClipRow pairs an Article with the EffectiveSource the UI should render.
type ClipRow struct {
	Article         model.Article
	EffectiveSource EffectiveSource
}

// ListClipped returns articles in the user's clip pseudo-feed (feed_type='clip').
// Star-saved articles (user_preferences.signal_type='save') are NOT included;
// they're reached via the 已保存 checkbox on /articles instead.
func (r *ClipRepository) ListClipped(q ClipQuery) ([]ClipRow, int, error) {
	args := []interface{}{q.UserID}
	where := []string{`f.feed_type = 'clip' AND f.owner_id = $1`}
	// Tenancy guard kept for symmetry with other queries in this codebase.
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
			where = append(where, `lower(regexp_replace(a.url, '^https?://(?:www\.)?([^/]+).*$', '\1')) = lower($`+strconv.Itoa(len(args))+`)`)
		}
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, q.Limit, q.Offset)
	limitParam := "$" + strconv.Itoa(len(args)-1)
	offsetParam := "$" + strconv.Itoa(len(args))
	rows, err := r.db.Query(`
		SELECT a.id, a.feed_id, f.title AS feed_title, COALESCE(f.feed_type, 'rss') AS feed_type,
		       a.title, a.url,
		       a.published_at, a.summary_brief, a.fetched_at,
		       COALESCE(a.word_count, 0), COALESCE(a.reading_minutes, 0),
		       COALESCE(a.media_type, ''),
		       COALESCE(rp.is_completed, false) AS is_read
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1
		WHERE `+whereSQL+`
		ORDER BY a.published_at DESC NULLS LAST, a.id DESC
		LIMIT `+limitParam+` OFFSET `+offsetParam, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []ClipRow
	for rows.Next() {
		var a model.Article
		var summary, mediaType sql.NullString
		var feedTitle sql.NullString
		var feedType string
		if err := rows.Scan(
			&a.ID, &a.FeedID, &feedTitle, &feedType, &a.Title, &a.URL,
			&a.PublishedAt, &summary, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes, &mediaType,
			&a.IsRead,
		); err != nil {
			return nil, 0, err
		}
		a.FeedTitle = feedTitle.String
		a.SummaryBrief = summary.String
		a.MediaType = mediaType.String
		out = append(out, ClipRow{
			Article:         a,
			EffectiveSource: effectiveSourceFor(a.FeedID, feedTitle.String, feedType, a.URL),
		})
	}
	return out, total, rows.Err()
}
