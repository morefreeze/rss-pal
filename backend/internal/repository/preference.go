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

func (r *PreferenceRepository) GetTopics() ([]model.InterestTopic, error) {
	query := `SELECT id, topic, weight, last_reinforced_at FROM interest_topics ORDER BY weight DESC`
	rows, err := r.db.Query(query)
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

func (r *PreferenceRepository) UpsertTopic(topic string, weightDelta float64) error {
	query := `
		INSERT INTO interest_topics (topic, weight, last_reinforced_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (topic) DO UPDATE SET
			weight = interest_topics.weight + $2,
			last_reinforced_at = NOW()
	`
	_, err := r.db.Exec(query, topic, weightDelta)
	return err
}

func (r *PreferenceRepository) DecayTopics(decayFactor float64) error {
	query := `UPDATE interest_topics SET weight = weight * $1 WHERE weight > 0.01`
	_, err := r.db.Exec(query, decayFactor)
	return err
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
