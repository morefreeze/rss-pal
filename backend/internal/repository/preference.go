package repository

import (
	"database/sql"

	"github.com/bytedance/rss-pal/internal/model"
)

type PreferenceRepository struct {
	db *sql.DB
}

func NewPreferenceRepository(db *sql.DB) *PreferenceRepository {
	return &PreferenceRepository{db: db}
}

func (r *PreferenceRepository) Add(preference *model.UserPreference) error {
	query := `INSERT INTO user_preferences (user_id, article_id, signal_type, signal_value) VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	return r.db.QueryRow(query, preference.UserID, preference.ArticleID, preference.SignalType, preference.SignalValue).Scan(&preference.ID, &preference.CreatedAt)
}

func (r *PreferenceRepository) GetTopics(userID int) ([]model.InterestTopic, error) {
	query := `SELECT id, topic, weight, last_reinforced_at FROM interest_topics WHERE user_id = $1 ORDER BY weight DESC`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []model.InterestTopic
	for rows.Next() {
		var t model.InterestTopic
		err := rows.Scan(&t.ID, &t.Topic, &t.Weight, &t.LastReinforcedAt)
		if err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, nil
}

func (r *PreferenceRepository) UpsertTopic(userID int, topic string, weightDelta float64) error {
	query := `
		INSERT INTO interest_topics (user_id, topic, weight, last_reinforced_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, topic) DO UPDATE SET
			weight = interest_topics.weight + $3,
			last_reinforced_at = NOW()
	`
	_, err := r.db.Exec(query, userID, topic, weightDelta)
	return err
}

func (r *PreferenceRepository) DecayTopics(userID int, decayFactor float64) error {
	query := `UPDATE interest_topics SET weight = weight * $1 WHERE user_id = $2 AND weight > 0.01`
	_, err := r.db.Exec(query, decayFactor, userID)
	return err
}

func (r *PreferenceRepository) GetTopicStrings(userID int) ([]string, error) {
	query := `SELECT topic FROM interest_topics WHERE user_id = $1 ORDER BY weight DESC LIMIT 20`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var topics []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, nil
}

func (r *PreferenceRepository) GetRecentReadTitles(userID int, limit int) ([]string, error) {
	query := `
		SELECT a.title
		FROM reading_progress rp
		JOIN articles a ON rp.article_id = a.id
		WHERE rp.user_id = $1
		ORDER BY rp.last_read_at DESC
		LIMIT $2
	`
	rows, err := r.db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		titles = append(titles, t)
	}
	return titles, nil
}

// DeleteSignal removes all rows for a given user+article+signal_type.
func (r *PreferenceRepository) DeleteSignal(userID, articleID int, signalType string) error {
	_, err := r.db.Exec(`DELETE FROM user_preferences WHERE user_id = $1 AND article_id = $2 AND signal_type = $3`, userID, articleID, signalType)
	return err
}

// GetUserSignals returns the most recent signal_value per signal_type for a given user+article.
func (r *PreferenceRepository) GetUserSignals(userID, articleID int) (map[string]float64, error) {
	query := `
		SELECT DISTINCT ON (signal_type) signal_type, signal_value
		FROM user_preferences
		WHERE user_id = $1 AND article_id = $2
		ORDER BY signal_type, created_at DESC
	`
	rows, err := r.db.Query(query, userID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	signals := map[string]float64{}
	for rows.Next() {
		var t string
		var v float64
		if err := rows.Scan(&t, &v); err != nil {
			return nil, err
		}
		signals[t] = v
	}
	return signals, nil
}

func (r *PreferenceRepository) GetArticleScore(articleID int) (float64, error) {
	query := `
		SELECT COALESCE(SUM(
			CASE signal_type
				WHEN 'like' THEN 5.0 * signal_value
				WHEN 'dislike' THEN -10.0 * signal_value
				WHEN 'save' THEN 3.0 * signal_value
				WHEN 'read_duration' THEN signal_value / 60.0
				ELSE 1.0 * signal_value
			END
		), 0)
		FROM user_preferences
		WHERE article_id = $1 AND created_at > NOW() - INTERVAL '30 days'
	`
	var score float64
	err := r.db.QueryRow(query, articleID).Scan(&score)
	return score, err
}
