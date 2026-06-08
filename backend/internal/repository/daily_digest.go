package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

type DailyDigest struct {
	UserID      int
	DayStart    time.Time
	IntroText   string
	ArticleIDs  []int64
	GeneratedAt time.Time
}

type DailyDigestRepository struct {
	db Querier
}

func NewDailyDigestRepository(db *sql.DB) *DailyDigestRepository {
	return &DailyDigestRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *DailyDigestRepository) WithCtx(c ctxkey.CtxGetter) *DailyDigestRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &DailyDigestRepository{db: q}
		}
	}
	return r
}

func (r *DailyDigestRepository) Get(userID int, dayStart time.Time) (*DailyDigest, error) {
	var d DailyDigest
	var ids pq.Int64Array
	err := r.db.QueryRow(`
		SELECT user_id, day_start, intro_text, article_ids, generated_at
		FROM daily_digests WHERE user_id = $1 AND day_start = $2
	`, userID, dayStart).Scan(&d.UserID, &d.DayStart, &d.IntroText, &ids, &d.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ArticleIDs = ids
	return &d, nil
}

func (r *DailyDigestRepository) Upsert(userID int, dayStart time.Time, intro string, articleIDs []int) error {
	ids := make(pq.Int64Array, len(articleIDs))
	for i, id := range articleIDs {
		ids[i] = int64(id)
	}
	_, err := r.db.Exec(`
		INSERT INTO daily_digests (user_id, day_start, intro_text, article_ids)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, day_start) DO UPDATE SET
			intro_text = EXCLUDED.intro_text,
			article_ids = EXCLUDED.article_ids,
			generated_at = NOW()
	`, userID, dayStart, intro, ids)
	return err
}

// UserIDsMissing returns user IDs that do not yet have a daily_digests row
// for `dayStart`. Used by the worker to pick up users it hasn't generated for.
func (r *DailyDigestRepository) UserIDsMissing(dayStart time.Time) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT u.id FROM users u
		LEFT JOIN daily_digests d ON d.user_id = u.id AND d.day_start = $1
		WHERE d.id IS NULL
		ORDER BY u.id
	`, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListDaysInRange returns the day_start values this user has digests for
// where from ≤ day_start ≤ to. Ordered ascending. Used by the briefing
// index endpoint to paint the calendar.
func (r *DailyDigestRepository) ListDaysInRange(userID int, from, to time.Time) ([]time.Time, error) {
	rows, err := r.db.Query(`
		SELECT day_start FROM daily_digests
		WHERE user_id = $1 AND day_start BETWEEN $2 AND $3
		ORDER BY day_start
	`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
