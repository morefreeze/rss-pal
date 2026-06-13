package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

// RefreshToken row. The plaintext token is never stored — only its sha256 hex
// digest in TokenHash. Issue() returns the plaintext to the caller exactly
// once at creation time; verification compares hashes only.
type RefreshToken struct {
	ID         int
	UserID     int
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
	UserAgent  string
	RevokedAt  sql.NullTime
}

type RefreshTokenRepository struct {
	db Querier
}

func NewRefreshTokenRepository(db *sql.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) WithCtx(c ctxkey.CtxGetter) *RefreshTokenRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &RefreshTokenRepository{db: q}
		}
	}
	return r
}

// HashRefreshToken returns the sha256 hex digest used as the stored
// identifier. Exported so the auth handler can compute it once and pass
// through to Find / Touch / Revoke without re-hashing.
func HashRefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// generateRefreshToken produces a 32-byte URL-safe random string. ~192 bits of
// entropy; collisions across the table are negligible.
func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Issue creates a new refresh token row and returns the plaintext token (the
// only time it is ever in scope) plus the persisted row. ttl is the lifetime
// from now; userAgent is recorded for the (future) "my devices" UI.
func (r *RefreshTokenRepository) Issue(userID int, ttl time.Duration, userAgent string) (plaintext string, row *RefreshToken, err error) {
	plaintext, err = generateRefreshToken()
	if err != nil {
		return "", nil, err
	}
	hash := HashRefreshToken(plaintext)
	expiresAt := time.Now().Add(ttl)
	row = &RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expiresAt,
		UserAgent: userAgent,
	}
	err = r.db.QueryRow(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		userID, hash, expiresAt, userAgent,
	).Scan(&row.ID, &row.CreatedAt)
	if err != nil {
		return "", nil, err
	}
	return plaintext, row, nil
}

// ErrInvalidRefreshToken covers all the "user must re-login" cases: unknown
// hash, expired, or revoked. The handler maps this to 401 without leaking
// which sub-case it was.
var ErrInvalidRefreshToken = errors.New("refresh token invalid, expired, or revoked")

// FindActiveByHash returns the row if and only if it is unrevoked and unexpired.
// Returns ErrInvalidRefreshToken in every failure mode; callers should not
// distinguish.
func (r *RefreshTokenRepository) FindActiveByHash(hash string) (*RefreshToken, error) {
	row := &RefreshToken{}
	err := r.db.QueryRow(
		`SELECT id, user_id, token_hash, expires_at, created_at, last_used_at, user_agent, revoked_at
		 FROM refresh_tokens
		 WHERE token_hash = $1`,
		hash,
	).Scan(&row.ID, &row.UserID, &row.TokenHash, &row.ExpiresAt, &row.CreatedAt,
		&row.LastUsedAt, &row.UserAgent, &row.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, err
	}
	if row.RevokedAt.Valid {
		return nil, ErrInvalidRefreshToken
	}
	if time.Now().After(row.ExpiresAt) {
		return nil, ErrInvalidRefreshToken
	}
	return row, nil
}

// TouchLastUsed bumps last_used_at = now() so a future "my devices" UI can
// show last activity. Failure is non-fatal — caller should log and continue.
func (r *RefreshTokenRepository) TouchLastUsed(id int) error {
	_, err := r.db.Exec(`UPDATE refresh_tokens SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

// RevokeByHash marks the row revoked. Used by /api/auth/logout.
func (r *RefreshTokenRepository) RevokeByHash(hash string) error {
	_, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked_at = NOW()
		 WHERE token_hash = $1 AND revoked_at IS NULL`,
		hash,
	)
	return err
}

// RevokeAllForUser is the "log out all devices" primitive. Returns count
// revoked for caller-side logging.
func (r *RefreshTokenRepository) RevokeAllForUser(userID int) (int64, error) {
	res, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked_at = NOW()
		 WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteExpired prunes long-expired rows. Daily cleanup task. Grace period of
// 7 days past expiry is kept so we can answer "why was I kicked off" support
// requests; before that we'd lose audit trail.
func (r *RefreshTokenRepository) DeleteExpired() (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL '7 days'`,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
