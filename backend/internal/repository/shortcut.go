package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

var ErrShortcutConnectionInvalid = errors.New("invalid shortcut connection")

type ShortcutRepository struct{ db Querier }

func NewShortcutRepository(db *sql.DB) *ShortcutRepository { return &ShortcutRepository{db: db} }
func (r *ShortcutRepository) WithCtx(c ctxkey.CtxGetter) *ShortcutRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &ShortcutRepository{db: q}
		}
	}
	return r
}
func shortcutSecret() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func shortcutDigest(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
func validShortcutSecret(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

// The user row serializes pairing, exchange, and revoke, so a concurrent
// exchange cannot resurrect credentials after a completed revocation.
func lockShortcutUser(q Querier, uid int) error {
	var id int
	return q.QueryRow(`SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&id)
}
func (r *ShortcutRepository) Pair(uid int) (string, time.Time, error) {
	code, err := shortcutSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return "", time.Time{}, err
	}
	defer rollback()
	if err = lockShortcutUser(tx, uid); err != nil {
		return "", time.Time{}, err
	}
	var expires time.Time
	err = tx.QueryRow(`INSERT INTO shortcut_pairings(user_id,code_hash,expires_at) VALUES($1,$2,now()+interval '5 minutes') ON CONFLICT(user_id) DO UPDATE SET code_hash=excluded.code_hash,expires_at=excluded.expires_at RETURNING expires_at`, uid, shortcutDigest(code)).Scan(&expires)
	if err != nil {
		return "", time.Time{}, err
	}
	if err = commit(); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}
func (r *ShortcutRepository) Connect(code string) (string, error) {
	if !validShortcutSecret(code) {
		return "", ErrShortcutConnectionInvalid
	}
	token, err := shortcutSecret()
	if err != nil {
		return "", err
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return "", err
	}
	defer rollback()
	var uid int
	err = tx.QueryRow(`SELECT user_id FROM shortcut_pairings WHERE code_hash=$1 AND expires_at>clock_timestamp()`, shortcutDigest(code)).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrShortcutConnectionInvalid
	}
	if err != nil {
		return "", err
	}
	if err = lockShortcutUser(tx, uid); err != nil {
		return "", err
	}
	err = tx.QueryRow(`DELETE FROM shortcut_pairings WHERE user_id=$1 AND code_hash=$2 AND expires_at>clock_timestamp() RETURNING user_id`, uid, shortcutDigest(code)).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrShortcutConnectionInvalid
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO shortcut_connections(user_id,token_hash) VALUES($1,$2)`, uid, shortcutDigest(token)); err != nil {
		return "", err
	}
	if err = commit(); err != nil {
		return "", err
	}
	return token, nil
}
func (r *ShortcutRepository) Count(uid int) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT count(*) FROM shortcut_connections WHERE user_id=$1`, uid).Scan(&n)
	return n, err
}
func (r *ShortcutRepository) Revoke(uid int) error {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return err
	}
	defer rollback()
	if err = lockShortcutUser(tx, uid); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM shortcut_pairings WHERE user_id=$1`, uid); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM shortcut_connections WHERE user_id=$1`, uid); err != nil {
		return err
	}
	return commit()
}

// Only the capture route uses this resolver. A capture key is never a JWT or
// a bookmarklet key. Hold a row lock until capture commits, serializing revoke.
func ResolveShortcutOwner(tx *sql.Tx, token string) (int, error) {
	if !validShortcutSecret(token) {
		return 0, ErrShortcutConnectionInvalid
	}
	digest := shortcutDigest(token)
	var uid int
	err := tx.QueryRow(`SELECT user_id FROM shortcut_connections WHERE token_hash=$1`, digest).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrShortcutConnectionInvalid
	}
	if err != nil {
		return 0, err
	}
	// All connection operations lock the user before a token. Capture only needs
	// KEY SHARE; concurrent captures remain possible, while revoke waits safely.
	var locked int
	err = tx.QueryRow(`SELECT id FROM users WHERE id=$1 FOR KEY SHARE`, uid).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrShortcutConnectionInvalid
	}
	if err != nil {
		return 0, err
	}
	err = tx.QueryRow(`SELECT user_id FROM shortcut_connections WHERE token_hash=$1 AND user_id=$2 FOR KEY SHARE`, digest, uid).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrShortcutConnectionInvalid
	}
	return uid, err
}
