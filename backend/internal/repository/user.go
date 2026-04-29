package repository

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateAdmin(username, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{Username: username, PasswordHash: string(hash), IsAdmin: true}
	err = r.db.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING id, created_at`,
		user.Username, user.PasswordHash, user.IsAdmin,
	).Scan(&user.ID, &user.CreatedAt)
	return user, err
}

func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = $1`, username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) FindByID(id int) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
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

func (r *UserRepository) Register(username, password string, code string) (*model.User, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Validate invite code
	var codeID int
	var usedBy *int
	err = tx.QueryRow(
		`SELECT id, used_by FROM invite_codes WHERE code = $1 AND (expires_at IS NULL OR expires_at > NOW())`,
		code,
	).Scan(&codeID, &usedBy)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid or expired invite code")
	}
	if err != nil {
		return nil, err
	}
	if usedBy != nil {
		return nil, fmt.Errorf("invite code already used")
	}

	// Check username uniqueness
	var exists bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("username already taken")
	}

	// Create user
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{Username: username, PasswordHash: string(hash), IsAdmin: false}
	err = tx.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING id, created_at`,
		user.Username, user.PasswordHash, user.IsAdmin,
	).Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Mark invite code as used
	_, err = tx.Exec(`UPDATE invite_codes SET used_by = $1 WHERE id = $2`, user.ID, codeID)
	if err != nil {
		return nil, err
	}

	return user, tx.Commit()
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

func generateCode(length int) string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return "RSS-" + string(b)
}
