package repository_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRefreshTokenRepository_IssueAndFind(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var userID int
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ('rt-user', 'x') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	repo := repository.NewRefreshTokenRepository(db)
	plaintext, row, err := repo.Issue(userID, 24*time.Hour, "Mozilla/5.0 test-agent")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if plaintext == "" {
		t.Fatal("Issue returned empty plaintext")
	}
	if row.UserID != userID {
		t.Errorf("row.UserID=%d, want %d", row.UserID, userID)
	}
	if row.UserAgent != "Mozilla/5.0 test-agent" {
		t.Errorf("row.UserAgent=%q", row.UserAgent)
	}
	if row.TokenHash == plaintext {
		t.Error("plaintext leaked into TokenHash column")
	}

	// Reading back by hash should succeed and yield the same row.
	got, err := repo.FindActiveByHash(repository.HashRefreshToken(plaintext))
	if err != nil {
		t.Fatalf("FindActiveByHash: %v", err)
	}
	if got.ID != row.ID {
		t.Errorf("FindActiveByHash id=%d, want %d", got.ID, row.ID)
	}
}

func TestRefreshTokenRepository_RevokedAndExpiredAreRejected(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var userID int
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ('rt-revoked', 'x') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	repo := repository.NewRefreshTokenRepository(db)

	// Revoked token → ErrInvalidRefreshToken.
	plain1, _, err := repo.Issue(userID, time.Hour, "")
	if err != nil {
		t.Fatalf("Issue revoked: %v", err)
	}
	hash1 := repository.HashRefreshToken(plain1)
	if err := repo.RevokeByHash(hash1); err != nil {
		t.Fatalf("RevokeByHash: %v", err)
	}
	if _, err := repo.FindActiveByHash(hash1); err != repository.ErrInvalidRefreshToken {
		t.Errorf("after revoke: got err=%v, want ErrInvalidRefreshToken", err)
	}

	// Expired token → ErrInvalidRefreshToken.
	plain2, row2, err := repo.Issue(userID, time.Hour, "")
	if err != nil {
		t.Fatalf("Issue expiring: %v", err)
	}
	if _, err := db.Exec(`UPDATE refresh_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, row2.ID); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}
	if _, err := repo.FindActiveByHash(repository.HashRefreshToken(plain2)); err != repository.ErrInvalidRefreshToken {
		t.Errorf("after expiry: got err=%v, want ErrInvalidRefreshToken", err)
	}

	// Unknown hash → ErrInvalidRefreshToken (not a sql.ErrNoRows leak).
	if _, err := repo.FindActiveByHash(strings.Repeat("0", 64)); err != repository.ErrInvalidRefreshToken {
		t.Errorf("unknown hash: got err=%v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefreshTokenRepository_RevokeAllForUser(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var userA, userB int
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ('rt-all-a', 'x') RETURNING id`,
	).Scan(&userA); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ('rt-all-b', 'x') RETURNING id`,
	).Scan(&userB); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	repo := repository.NewRefreshTokenRepository(db)

	// 2 tokens for A, 1 for B.
	plainA1, _, _ := repo.Issue(userA, time.Hour, "")
	plainA2, _, _ := repo.Issue(userA, time.Hour, "")
	plainB1, _, _ := repo.Issue(userB, time.Hour, "")

	n, err := repo.RevokeAllForUser(userA)
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if n != 2 {
		t.Errorf("RevokeAllForUser revoked %d, want 2", n)
	}

	// A's tokens dead, B's still alive.
	if _, err := repo.FindActiveByHash(repository.HashRefreshToken(plainA1)); err != repository.ErrInvalidRefreshToken {
		t.Errorf("A1: got %v, want invalid", err)
	}
	if _, err := repo.FindActiveByHash(repository.HashRefreshToken(plainA2)); err != repository.ErrInvalidRefreshToken {
		t.Errorf("A2: got %v, want invalid", err)
	}
	if _, err := repo.FindActiveByHash(repository.HashRefreshToken(plainB1)); err != nil {
		t.Errorf("B1 should still be active: %v", err)
	}
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	h1 := repository.HashRefreshToken("abc")
	h2 := repository.HashRefreshToken("abc")
	if h1 != h2 {
		t.Error("HashRefreshToken must be deterministic")
	}
	if h1 == repository.HashRefreshToken("abd") {
		t.Error("HashRefreshToken must differ for different inputs")
	}
	if len(h1) != 64 {
		t.Errorf("expected hex sha256 length 64, got %d", len(h1))
	}
}
