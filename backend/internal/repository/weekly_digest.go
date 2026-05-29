package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

type WeeklyDigest struct {
	UserID      int
	WeekStart   time.Time
	IntroText   string
	ArticleIDs  []int64
	GeneratedAt time.Time
}

type WeeklyDigestRepository struct {
	db *sql.DB
}

func NewWeeklyDigestRepository(db *sql.DB) *WeeklyDigestRepository {
	return &WeeklyDigestRepository{db: db}
}

func (r *WeeklyDigestRepository) Get(userID int, weekStart time.Time) (*WeeklyDigest, error) {
	var d WeeklyDigest
	var ids pq.Int64Array
	err := r.db.QueryRow(`
		SELECT user_id, week_start, intro_text, article_ids, generated_at
		FROM weekly_digests WHERE user_id = $1 AND week_start = $2
	`, userID, weekStart).Scan(&d.UserID, &d.WeekStart, &d.IntroText, &ids, &d.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ArticleIDs = ids
	return &d, nil
}

func (r *WeeklyDigestRepository) Upsert(userID int, weekStart time.Time, intro string, articleIDs []int) error {
	ids := make(pq.Int64Array, len(articleIDs))
	for i, id := range articleIDs {
		ids[i] = int64(id)
	}
	_, err := r.db.Exec(`
		INSERT INTO weekly_digests (user_id, week_start, intro_text, article_ids)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, week_start) DO UPDATE SET
			intro_text = EXCLUDED.intro_text,
			article_ids = EXCLUDED.article_ids,
			generated_at = NOW()
	`, userID, weekStart, intro, ids)
	return err
}

// UserIDsMissing returns user IDs that do not yet have a weekly_digests row
// for `weekStart`. Mirrors DailyDigestRepository.UserIDsMissing.
func (r *WeeklyDigestRepository) UserIDsMissing(weekStart time.Time) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT u.id FROM users u
		LEFT JOIN weekly_digests d ON d.user_id = u.id AND d.week_start = $1
		WHERE d.id IS NULL
		ORDER BY u.id
	`, weekStart)
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
