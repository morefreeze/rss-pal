package repository

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

type ProgressRepository struct {
	db Querier
}

func NewProgressRepository(db *sql.DB) *ProgressRepository {
	return &ProgressRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *ProgressRepository) WithCtx(c ctxkey.CtxGetter) *ProgressRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &ProgressRepository{db: q}
		}
	}
	return r
}

func (r *ProgressRepository) GetByArticleAndUser(articleID, userID int) (*model.ReadingProgress, error) {
	query := `SELECT id, user_id, article_id, scroll_position, last_read_at, is_completed FROM reading_progress WHERE article_id = $1 AND user_id = $2`
	var p model.ReadingProgress
	err := r.db.QueryRow(query, articleID, userID).Scan(&p.ID, &p.UserID, &p.ArticleID, &p.ScrollPosition, &p.LastReadAt, &p.IsCompleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProgressUpsertResult exposes whether is_completed flipped false→true on this call.
type ProgressUpsertResult struct {
	NewlyCompleted bool
}

func (r *ProgressRepository) Upsert(progress *model.ReadingProgress) (ProgressUpsertResult, error) {
	var prev sql.NullBool
	_ = r.db.QueryRow(
		`SELECT is_completed FROM reading_progress WHERE article_id = $1 AND user_id = $2`,
		progress.ArticleID, progress.UserID,
	).Scan(&prev)

	query := `
		INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (article_id, user_id) DO UPDATE SET
			scroll_position = GREATEST(reading_progress.scroll_position, EXCLUDED.scroll_position),
			last_read_at = EXCLUDED.last_read_at,
			is_completed = reading_progress.is_completed OR EXCLUDED.is_completed
		RETURNING id, scroll_position, is_completed
	`
	err := r.db.QueryRow(query, progress.UserID, progress.ArticleID, progress.ScrollPosition, progress.LastReadAt, progress.IsCompleted).Scan(&progress.ID, &progress.ScrollPosition, &progress.IsCompleted)
	if err != nil {
		return ProgressUpsertResult{}, err
	}
	wasCompleted := prev.Valid && prev.Bool
	return ProgressUpsertResult{NewlyCompleted: !wasCompleted && progress.IsCompleted}, nil
}

func (r *ProgressRepository) Reset(articleID, userID int) error {
	query := `
		UPDATE reading_progress
		SET scroll_position = 0, last_read_at = NOW(), is_completed = false
		WHERE article_id = $1 AND user_id = $2
	`
	_, err := r.db.Exec(query, articleID, userID)
	return err
}

func (r *ProgressRepository) UpdateTimestamp(articleID int, t time.Time) error {
	query := `UPDATE reading_progress SET last_read_at = $1 WHERE article_id = $2`
	_, err := r.db.Exec(query, t, articleID)
	return err
}

// MarkAllReadClipFilter narrows mark-all-read to the same subset that the
// /api/clip list shows when the user is in 网摘 mode with a tag/source
// filter active. Zero value applies no clip-specific filtering.
type MarkAllReadClipFilter struct {
	TagIDs      []int
	Mode        string // "and" | "or"; only honored when len(TagIDs)>1
	Untagged    bool
	SourceKind  string // "" | "feed" | "host"
	SourceValue string
}

// MarkAllRead marks every article visible under the given filters as read.
// Filters mirror ArticleRepository.GetAll so the affected set matches what
// the user currently sees in the list. Pass feedID=nil / unreadOnly=false /
// savedOnly=false to apply across the whole library. clip carries the
// extra tag/source filters used by 网摘 mode.
func (r *ProgressRepository) MarkAllRead(userID int, feedID *int, unreadOnly, savedOnly bool, clip MarkAllReadClipFilter) error {
	args := []interface{}{userID}
	argIdx := 2
	joins := ""
	conditions := []string{"(f.owner_id IS NULL OR f.owner_id = $1)"}

	if feedID != nil {
		conditions = append(conditions, fmt.Sprintf("a.feed_id = $%d", argIdx))
		args = append(args, *feedID)
		argIdx++
	}
	if unreadOnly {
		joins += " LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1"
		conditions = append(conditions, "COALESCE(rp.is_completed, false) = false")
	}
	if savedOnly {
		joins += " LEFT JOIN user_preferences up_save ON up_save.article_id = a.id AND up_save.user_id = $1 AND up_save.signal_type = 'save'"
		conditions = append(conditions, fmt.Sprintf("up_save.signal_value = $%d", argIdx))
		args = append(args, 1.0)
		argIdx++
	}

	// Mirror ClipRepository.ListClipped's tag/source predicates so a
	// user-visible filter on /articles?view=clip stays in effect for
	// mark-all-read.
	if clip.Untagged {
		conditions = append(conditions, `NOT EXISTS (
			SELECT 1 FROM article_user_tags aut
			WHERE aut.article_id = a.id AND aut.user_id = $1
		)`)
	} else if len(clip.TagIDs) > 0 {
		args = append(args, pq.Array(clip.TagIDs))
		idsParam := "$" + strconv.Itoa(argIdx)
		argIdx++
		if clip.Mode == "and" && len(clip.TagIDs) > 1 {
			args = append(args, len(clip.TagIDs))
			countParam := "$" + strconv.Itoa(argIdx)
			argIdx++
			conditions = append(conditions, `(
				SELECT COUNT(DISTINCT aut.tag_id) FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			) = `+countParam)
		} else {
			conditions = append(conditions, `EXISTS (
				SELECT 1 FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			)`)
		}
	}
	switch clip.SourceKind {
	case "feed":
		if clip.SourceValue != "" {
			args = append(args, clip.SourceValue)
			conditions = append(conditions, "a.feed_id::text = $"+strconv.Itoa(argIdx))
			argIdx++
		}
	case "host":
		if clip.SourceValue != "" {
			args = append(args, clip.SourceValue)
			conditions = append(conditions, `lower(regexp_replace(a.url, '^https?://(?:www\.)?([^/]+).*$', '\1')) = lower($`+strconv.Itoa(argIdx)+`)`)
			argIdx++
		}
	}

	where := conditions[0]
	for i := 1; i < len(conditions); i++ {
		where += " AND " + conditions[i]
	}

	query := `
		INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
		SELECT $1, a.id, 1.0, NOW(), true
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id` + joins + `
		WHERE ` + where + `
		ON CONFLICT (article_id, user_id) DO UPDATE SET
			is_completed = true, scroll_position = 1.0, last_read_at = NOW()
	`
	_, err := r.db.Exec(query, args...)
	return err
}
