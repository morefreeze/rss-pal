package repository

import (
	"database/sql"
	"net/url"
	"strings"

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

// GetTopByUser returns the top-N interest topics for a user, ordered by weight DESC.
func (r *PreferenceRepository) GetTopByUser(userID, limit int) ([]model.InterestTopic, error) {
	rows, err := r.db.Query(`
		SELECT id, topic, weight, last_reinforced_at
		FROM interest_topics
		WHERE user_id = $1
		ORDER BY weight DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InterestTopic
	for rows.Next() {
		var t model.InterestTopic
		if err := rows.Scan(&t.ID, &t.Topic, &t.Weight, &t.LastReinforcedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
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

// UpsertCategory reinforces a user's interest weight for a coarse category.
// Mirror of UpsertTopic, against the interest_categories table. Same
// signal-weight semantics, so callers can reuse api.SignalToTopicWeight.
func (r *PreferenceRepository) UpsertCategory(userID int, category string, weightDelta float64) error {
	if category == "" {
		return nil
	}
	query := `
		INSERT INTO interest_categories (user_id, category, weight, last_reinforced_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, category) DO UPDATE SET
			weight = interest_categories.weight + $3,
			last_reinforced_at = NOW()
	`
	_, err := r.db.Exec(query, userID, category, weightDelta)
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

// GetArticleScore returns the per-article recommendation rank score over the
// last 30 days. Note that completed_listen carries weight 8.0 here (a 30-min
// listen is high-cost engagement and should rank a podcast strongly) but only
// 2.0 in topic-strength scoring, where it stays at parity with `save` so a
// single completed podcast doesn't dominate a user's interest profile.
func (r *PreferenceRepository) GetArticleScore(articleID int) (float64, error) {
	query := `
		SELECT COALESCE(SUM(
			CASE signal_type
				WHEN 'like' THEN 5.0 * signal_value
				WHEN 'dislike' THEN -10.0 * signal_value
				WHEN 'save' THEN 3.0 * signal_value
				WHEN 'read_duration' THEN signal_value / 60.0
				WHEN 'completed_listen' THEN 8.0 * signal_value
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

// --- interest_tags (mirror of interest_topics, finer grain) ---

func (r *PreferenceRepository) GetTags(userID int) ([]model.InterestTag, error) {
	rows, err := r.db.Query(
		`SELECT id, tag, weight, last_reinforced_at FROM interest_tags
		 WHERE user_id = $1 ORDER BY weight DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InterestTag
	for rows.Next() {
		var t model.InterestTag
		if err := rows.Scan(&t.ID, &t.Tag, &t.Weight, &t.LastReinforcedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (r *PreferenceRepository) UpsertTag(userID int, tag string, weightDelta float64) error {
	if tag == "" {
		return nil
	}
	_, err := r.db.Exec(`
		INSERT INTO interest_tags (user_id, tag, weight, last_reinforced_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, tag) DO UPDATE SET
		  weight = interest_tags.weight + $3,
		  last_reinforced_at = NOW()
	`, userID, tag, weightDelta)
	return err
}

func (r *PreferenceRepository) DeleteTopic(userID, id int) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM interest_topics WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PreferenceRepository) DeleteTag(userID, id int) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM interest_tags WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DampenTopic reduces the weight of an existing interest_topics row, clamped at 0.
// No-op if the (user_id, topic) row does not exist — disliking a topic the user has
// never engaged with should not create a zero-weight entry. Only acts on negative deltas.
func (r *PreferenceRepository) DampenTopic(userID int, topic string, delta float64) error {
	if topic == "" || delta >= 0 {
		return nil
	}
	_, err := r.db.Exec(`
		UPDATE interest_topics
		SET weight = GREATEST(weight + $3, 0), last_reinforced_at = NOW()
		WHERE user_id = $1 AND topic = $2
	`, userID, topic, delta)
	return err
}

// DampenTag mirrors DampenTopic for interest_tags.
func (r *PreferenceRepository) DampenTag(userID int, tag string, delta float64) error {
	if tag == "" || delta >= 0 {
		return nil
	}
	_, err := r.db.Exec(`
		UPDATE interest_tags
		SET weight = GREATEST(weight + $3, 0), last_reinforced_at = NOW()
		WHERE user_id = $1 AND tag = $2
	`, userID, tag, delta)
	return err
}

// --- decay (all users) ---

func (r *PreferenceRepository) DecayAllTopics(factor float64) error {
	_, err := r.db.Exec(
		`UPDATE interest_topics SET weight = weight * $1 WHERE weight > 0.01`, factor)
	return err
}

func (r *PreferenceRepository) DecayAllTags(factor float64) error {
	_, err := r.db.Exec(
		`UPDATE interest_tags SET weight = weight * $1 WHERE weight > 0.01`, factor)
	return err
}

// --- signal strength aggregation (used by worker) ---

type UserSignalStrength struct {
	UserID   int
	Strength float64
}

// GetUsersWithStrongSignal returns each user's MAX signal strength against an article.
// Used by the worker after classifying to attribute the topic/tags to all interested users.
func (r *PreferenceRepository) GetUsersWithStrongSignal(articleID int) ([]UserSignalStrength, error) {
	rows, err := r.db.Query(`
		SELECT user_id,
		       MAX(CASE signal_type
		           WHEN 'save' THEN 2.0
		           WHEN 'like' THEN 1.0
		           WHEN 'completed_listen' THEN 2.0
		           WHEN 'read_duration' THEN
		             CASE WHEN signal_value >= 60 THEN 0.5 ELSE 0 END
		           ELSE 0
		       END) AS strength
		FROM user_preferences
		WHERE article_id = $1
		GROUP BY user_id
		HAVING MAX(CASE signal_type
		           WHEN 'save' THEN 2.0
		           WHEN 'like' THEN 1.0
		           WHEN 'completed_listen' THEN 2.0
		           WHEN 'read_duration' THEN
		             CASE WHEN signal_value >= 60 THEN 0.5 ELSE 0 END
		           ELSE 0
		       END) > 0
	`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSignalStrength
	for rows.Next() {
		var u UserSignalStrength
		if err := rows.Scan(&u.UserID, &u.Strength); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// GetUserSignalHosts returns hosts of articles the user liked/saved, disliked,
// or completed-read in the last 30 days.
func (r *PreferenceRepository) GetUserSignalHosts(userID int) (*model.HostSignalSet, error) {
	out := &model.HostSignalSet{
		Liked:     map[string]struct{}{},
		Disliked:  map[string]struct{}{},
		Completed: map[string]struct{}{},
	}
	rows, err := r.db.Query(`
		SELECT a.url, p.signal_type, COALESCE(p.signal_value, 1.0)
		FROM user_preferences p
		JOIN articles a ON a.id = p.article_id
		WHERE p.user_id = $1
		  AND p.created_at > NOW() - INTERVAL '30 days'
		  AND p.signal_type IN ('like', 'save', 'dislike')
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rawURL, sigType string
		var sigVal float64
		if err := rows.Scan(&rawURL, &sigType, &sigVal); err != nil {
			return nil, err
		}
		h := hostOfURL(rawURL)
		if h == "" {
			continue
		}
		switch sigType {
		case "like", "save":
			if sigVal > 0 {
				out.Liked[h] = struct{}{}
			}
		case "dislike":
			if sigVal > 0 {
				out.Disliked[h] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	crows, err := r.db.Query(`
		SELECT a.url FROM reading_progress rp
		JOIN articles a ON a.id = rp.article_id
		WHERE rp.user_id = $1 AND rp.is_completed = true
		  AND rp.last_read_at > NOW() - INTERVAL '30 days'
	`, userID)
	if err != nil {
		return out, nil // not fatal
	}
	defer crows.Close()
	for crows.Next() {
		var rawURL string
		if err := crows.Scan(&rawURL); err == nil {
			if h := hostOfURL(rawURL); h != "" {
				out.Completed[h] = struct{}{}
			}
		}
	}
	return out, nil
}

func hostOfURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
