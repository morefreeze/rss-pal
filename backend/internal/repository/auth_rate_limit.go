package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AuthRateLimitRepository keeps budgets across process restarts and replicas.
// No raw IP, username, password or token is stored here.
type AuthRateLimitRepository struct{ db *sql.DB }

func NewAuthRateLimitRepository(db *sql.DB) *AuthRateLimitRepository {
	return &AuthRateLimitRepository{db: db}
}

// Allow consumes one attempt atomically. Rejections never extend the window.
// Database time is authoritative, so host clock skew cannot reset budgets.
func (r *AuthRateLimitRepository) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if key == "" || limit < 1 || window < time.Second {
		return false, 0, errors.New("invalid auth rate limit")
	}
	var count, seconds int
	err := r.db.QueryRowContext(ctx, `
 INSERT INTO auth_rate_limits (bucket_key, attempts, expires_at)
 VALUES ($1, 1, statement_timestamp() + $3 * interval '1 second')
 ON CONFLICT (bucket_key) DO UPDATE SET
   attempts = CASE WHEN auth_rate_limits.expires_at <= statement_timestamp() THEN 1
                   ELSE LEAST(auth_rate_limits.attempts + 1, $2 + 1) END,
   expires_at = CASE WHEN auth_rate_limits.expires_at <= statement_timestamp()
                    THEN statement_timestamp() + $3 * interval '1 second'
                    ELSE auth_rate_limits.expires_at END
 RETURNING attempts, GREATEST(1, CEIL(EXTRACT(EPOCH FROM (expires_at - statement_timestamp())))::integer)`,
		key, limit, int(window/time.Second)).Scan(&count, &seconds)
	if err != nil {
		return false, 0, err
	}
	return count <= limit, time.Duration(seconds) * time.Second, nil
}

// Prune is bounded so cleanup cannot monopolize database resources.
func (r *AuthRateLimitRepository) Prune(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_rate_limits WHERE bucket_key IN
 (SELECT bucket_key FROM auth_rate_limits WHERE expires_at < NOW() ORDER BY expires_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`)
	return err
}
