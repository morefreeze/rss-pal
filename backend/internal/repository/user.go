package repository

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"golang.org/x/crypto/bcrypt"
)

type UserRepository struct {
	db Querier
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present (e.g. pre-auth handlers like login/register
// that run before the RLS middleware). This is safe because UserRepository
// queries do not depend on RLS for authentication paths.
//
// TODO(rls-phase-3): the users table itself will need a policy exemption or
// the migration must run with bypass; pre-auth Register has no app.user_id
// in context.
func (r *UserRepository) WithCtx(c ctxkey.CtxGetter) *UserRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &UserRepository{db: q}
		}
	}
	return r
}

func (r *UserRepository) CreateAdmin(username, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Founding admin sees the full shared backlog (epoch). Regular users that
	// register later get the default 7-day floor from the column default.
	user := &model.User{Username: username, PasswordHash: string(hash), IsAdmin: true}
	err = r.db.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin, shared_visible_from)
		 VALUES ($1, $2, $3, '1970-01-01'::TIMESTAMP)
		 RETURNING id, created_at, shared_visible_from`,
		user.Username, user.PasswordHash, user.IsAdmin,
	).Scan(&user.ID, &user.CreatedAt, &user.SharedVisibleFrom)
	return user, err
}

// UpdateSharedVisibleFrom moves the calling user's shared-content floor to
// NOW() - daysBack days. daysBack must be >= 0. Returns the new floor value.
func (r *UserRepository) UpdateSharedVisibleFrom(userID, daysBack int) (time.Time, error) {
	if daysBack < 0 {
		return time.Time{}, fmt.Errorf("days_back must be >= 0")
	}
	var floor time.Time
	err := r.db.QueryRow(
		`UPDATE users SET shared_visible_from = NOW() - ($1 || ' days')::INTERVAL
		   WHERE id = $2
		 RETURNING shared_visible_from`,
		daysBack, userID,
	).Scan(&floor)
	return floor, err
}

func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at, shared_visible_from FROM users WHERE username = $1`, username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt, &user.SharedVisibleFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) FindByID(id int) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at, shared_visible_from FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt, &user.SharedVisibleFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) VerifyPassword(user *model.User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
}

func (r *UserRepository) AdminExists() (bool, error) {
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE is_admin = true)`).Scan(&exists)
	return exists, err
}

func (r *UserRepository) ChangePassword(userID int, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), userID)
	return err
}

func (r *UserRepository) Register(username, password string, code string) (*model.User, error) {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()

	// Validate invite code
	var codeID int
	var usedBy *int
	err = tx.QueryRow(
		`SELECT id, used_by FROM invite_codes WHERE code = $1 AND (expires_at IS NULL OR expires_at > NOW())`,
		code,
	).Scan(&codeID, &usedBy)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("邀请码无效或已过期")
	}
	if err != nil {
		return nil, err
	}
	if usedBy != nil {
		return nil, fmt.Errorf("邀请码已被使用")
	}

	// Check username uniqueness
	var exists bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("用户名已被占用")
	}

	// Create user
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{Username: username, PasswordHash: string(hash), IsAdmin: false}
	err = tx.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING id, created_at, shared_visible_from`,
		user.Username, user.PasswordHash, user.IsAdmin,
	).Scan(&user.ID, &user.CreatedAt, &user.SharedVisibleFrom)
	if err != nil {
		return nil, err
	}

	// Mark invite code as used
	_, err = tx.Exec(`UPDATE invite_codes SET used_by = $1 WHERE id = $2`, user.ID, codeID)
	if err != nil {
		return nil, err
	}

	if err := commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) CreateInviteCode(createdBy int, expiresInHours int) (*model.InviteCode, error) {
	code := generateCode(8)
	var expiresAt *time.Time
	if expiresInHours > 0 {
		t := time.Now().Add(time.Duration(expiresInHours) * time.Hour)
		expiresAt = &t
	}

	ic := &model.InviteCode{CreatedBy: createdBy, ExpiresAt: expiresAt}
	err := r.db.QueryRow(
		`INSERT INTO invite_codes (code, created_by, expires_at) VALUES ($1, $2, $3) RETURNING id, created_at`,
		code, createdBy, expiresAt,
	).Scan(&ic.ID, &ic.CreatedAt)
	ic.Code = code
	return ic, err
}

func (r *UserRepository) ListInviteCodes() ([]model.InviteCode, error) {
	rows, err := r.db.Query(
		`SELECT id, code, created_by, used_by, expires_at, created_at FROM invite_codes ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []model.InviteCode
	for rows.Next() {
		var c model.InviteCode
		err := rows.Scan(&c.ID, &c.Code, &c.CreatedBy, &c.UsedBy, &c.ExpiresAt, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, nil
}

func (r *UserRepository) CountNonAdminUsers() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = false`).Scan(&count)
	return count, err
}

// GetByBookmarkletToken returns the user that owns the given bookmarklet
// token, or (nil, nil) if no row matches. Used by the capture endpoint
// to authenticate cross-origin bookmarklet requests.
func (r *UserRepository) GetByBookmarkletToken(token string) (*model.User, error) {
	if token == "" {
		return nil, nil
	}
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at, shared_visible_from FROM users WHERE bookmarklet_token = $1`,
		token,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt, &user.SharedVisibleFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

// GetBookmarkletToken returns the user's current bookmarklet token, or "" if
// none has been generated yet.
func (r *UserRepository) GetBookmarkletToken(userID int) (string, error) {
	var token sql.NullString
	err := r.db.QueryRow(`SELECT bookmarklet_token FROM users WHERE id = $1`, userID).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return token.String, err
}

// SetBookmarkletToken writes (or rotates) the user's long-lived bookmarklet
// token. Pass an empty string to clear it.
func (r *UserRepository) SetBookmarkletToken(userID int, token string) error {
	var t interface{} = token
	if token == "" {
		t = nil
	}
	_, err := r.db.Exec(`UPDATE users SET bookmarklet_token = $1 WHERE id = $2`, t, userID)
	return err
}

// GetBriefingLastTab returns the user's most recently viewed briefing tab,
// defaulting to "daily" if the column is somehow null.
func (r *UserRepository) GetBriefingLastTab(userID int) (string, error) {
	var tab sql.NullString
	err := r.db.QueryRow(`SELECT briefing_last_tab FROM users WHERE id = $1`, userID).Scan(&tab)
	if err == sql.ErrNoRows {
		return "daily", nil
	}
	if err != nil {
		return "", err
	}
	if !tab.Valid || tab.String == "" {
		return "daily", nil
	}
	return tab.String, nil
}

// SetBriefingLastTab persists the user's briefing tab choice.
// Caller must validate `tab` ∈ {"daily","weekly"} before calling.
func (r *UserRepository) SetBriefingLastTab(userID int, tab string) error {
	_, err := r.db.Exec(`UPDATE users SET briefing_last_tab = $1 WHERE id = $2`, tab, userID)
	return err
}

// ListAll returns every user (id-ordered). Used by daily cron.
func (r *UserRepository) ListAll() ([]model.User, error) {
	rows, err := r.db.Query(`SELECT id, username, COALESCE(is_admin, false) FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func generateCode(length int) string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return "RSS-" + string(b)
}
