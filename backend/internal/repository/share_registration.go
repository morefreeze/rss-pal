package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/registrationpolicy"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidRegistrationShare = errors.New("invalid registration share")

// A token public ID must only be supplied after verifying the share signature.
type ShareRegistrationSource struct{ ShortCode, PublicID, LegacyDigest string }

// RegisterFromShare creates an independent ordinary account. The share lock
// serializes admission with revocation/deletion, but does not consume the link.
func (r *UserRepository) RegisterFromShare(ctx context.Context, username, password string, source ShareRegistrationSource, policy registrationpolicy.Policy) (*model.User, error) {
	column, value, count := "", "", 0
	for name, v := range map[string]string{"short_code": source.ShortCode, "public_id": source.PublicID, "legacy_token_digest": source.LegacyDigest} {
		if v != "" {
			column, value, count = name, v, count+1
		}
	}
	if count != 1 {
		return nil, ErrInvalidRegistrationShare
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	var id string
	var owner int
	// column comes only from the fixed map above, never from caller SQL.
	err = tx.QueryRowContext(ctx, `SELECT public_id,created_by FROM article_shares WHERE `+column+`=$1 FOR SHARE`, value).Scan(&id, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidRegistrationShare
	}
	if err != nil {
		return nil, err
	}
	if !policy.Allows(owner) {
		return nil, ErrInvalidRegistrationShare
	}
	var active bool
	// Check current database time AFTER acquiring the lock (a wait may cross expiry).
	err = tx.QueryRowContext(ctx, `SELECT revoked_at IS NULL AND (expires_at IS NULL OR expires_at > clock_timestamp()) FROM article_shares WHERE public_id=$1`, id).Scan(&active)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrInvalidRegistrationShare
	}
	user := &model.User{Username: username, PasswordHash: string(hash), IsAdmin: false}
	err = tx.QueryRowContext(ctx, `INSERT INTO users(username,password_hash,is_admin) VALUES($1,$2,false) RETURNING id,created_at,shared_visible_from`, username, string(hash)).Scan(&user.ID, &user.CreatedAt, &user.SharedVisibleFrom)
	if err != nil {
		return nil, err
	}
	if err = commit(); err != nil {
		return nil, err
	}
	return user, nil
}
