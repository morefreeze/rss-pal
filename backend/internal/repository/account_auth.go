package repository

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrAccountAuth = errors.New("authentication request invalid or expired")
var ErrAccountRateLimit = errors.New("please wait before requesting another code")

type AccountAuthRepository struct{ db *sql.DB }

func NewAccountAuthRepository(db *sql.DB) *AccountAuthRepository {
	return &AccountAuthRepository{db: db}
}
func resolveIdentity(ctx context.Context, tx *sql.Tx, provider, subject, email string, bind int, signup bool) (int, error) {
	// Serialize a provider subject (and each target user's provider slot) before resolving.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, provider+":"+subject); err != nil {
		return 0, err
	}
	var uid int
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM account_identities WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&uid)
	if err == nil {
		if bind != 0 && bind != uid {
			return 0, ErrAccountAuth
		}
		return uid, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	if bind > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, bind).Scan(&uid); err != nil {
			return 0, ErrAccountAuth
		}
	} else {
		if !signup {
			return 0, ErrAccountAuth
		}
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return 0, err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO users(username,password_hash,is_admin,nickname_setup_required) VALUES($1,'!external',false,true) RETURNING id`, "reader_"+hex.EncodeToString(b)).Scan(&uid); err != nil {
			return 0, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_identities(provider,subject,user_id,email) VALUES($1,$2,$3,$4)`, provider, subject, uid, email)
	return uid, err
}
func (r *AccountAuthRepository) ResolveIdentity(ctx context.Context, p, s, e string, bind int, signup bool) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := resolveIdentity(ctx, tx, p, s, e, bind, signup)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (r *AccountAuthRepository) CreateEmailChallenge(ctx context.Context, id, email, hash, ip string, bind int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A global transaction lock makes all three budgets atomic across API replicas.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(674064)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_email_challenges WHERE created_at < now()-interval '1 day'`); err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM account_email_challenges WHERE email=$1 AND created_at>now()-interval '60 seconds') AND (SELECT count(*) FROM account_email_challenges WHERE email=$1 AND created_at>now()-interval '1 hour')<5 AND (SELECT count(*) FROM account_email_challenges WHERE ip_hash=$2 AND created_at>now()-interval '1 hour')<20 AND (SELECT count(*) FROM account_email_challenges WHERE created_at>now()-interval '1 hour')<200`, email, ip).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAccountRateLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_email_challenges(id,email,code_hash,ip_hash,bind_user_id) VALUES($1,$2,$3,$4,$5)`, id, email, hash, ip, bind)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AccountAuthRepository) InvalidateEmail(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE account_email_challenges SET consumed=true WHERE id=$1`, id)
	return err
}
func (r *AccountAuthRepository) VerifyEmailChallenge(ctx context.Context, id, hash string, bind int, signup bool) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var email, want string
	var owner, attempts int
	var expires time.Time
	var consumed bool
	err = tx.QueryRowContext(ctx, `SELECT email,code_hash,bind_user_id,attempts,expires_at,consumed FROM account_email_challenges WHERE id=$1 FOR UPDATE`, id).Scan(&email, &want, &owner, &attempts, &expires, &consumed)
	if err != nil || owner != bind || consumed || attempts >= 5 || !time.Now().Before(expires) {
		return 0, ErrAccountAuth
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(want)) != 1 {
		if _, err = tx.ExecContext(ctx, `UPDATE account_email_challenges SET attempts=attempts+1 WHERE id=$1`, id); err != nil {
			return 0, err
		}
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return 0, ErrAccountAuth
	}
	uid, err := resolveIdentity(ctx, tx, "email", email, email, bind, signup)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_email_challenges SET consumed=true WHERE id=$1`, id); err != nil {
		return 0, err
	}
	return uid, tx.Commit()
}

type AccountIdentity struct {
	Provider  string    `json:"provider"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *AccountAuthRepository) Identities(ctx context.Context, uid int) ([]AccountIdentity, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT provider,email,created_at FROM account_identities WHERE user_id=$1 ORDER BY created_at`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountIdentity{}
	for rows.Next() {
		var i AccountIdentity
		if err := rows.Scan(&i.Provider, &i.Email, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (r *AccountAuthRepository) PutFlow(ctx context.Context, hash, kind, browser string, payload any) error {
	_, _ = r.db.ExecContext(ctx, `DELETE FROM account_auth_flows WHERE expires_at < now()-interval '1 day'`)
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO account_auth_flows(token_hash,kind,browser_hash,payload,expires_at) VALUES($1,$2,$3,$4,now()+CASE WHEN $2='completion' THEN interval '2 minutes' ELSE interval '10 minutes' END)`, hash, kind, browser, b)
	return err
}
func (r *AccountAuthRepository) TakeFlow(ctx context.Context, hash, kind, browser string, out any) error {
	var b []byte
	err := r.db.QueryRowContext(ctx, `DELETE FROM account_auth_flows WHERE token_hash=$1 AND kind=$2 AND browser_hash=$3 AND expires_at>now() RETURNING payload`, hash, kind, browser).Scan(&b)
	if err != nil {
		return ErrAccountAuth
	}
	return json.Unmarshal(b, out)
}
