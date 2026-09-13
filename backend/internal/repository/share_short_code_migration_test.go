package repository_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestMigration041ArticleShareShortCodes(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "040_explore_provider_materialized_at.sql")
	defer cleanup()

	userID, feedID, articleID := insertArticleShareMigrationFixture(t, db)
	publicID := deterministicArticleSharePublicID("historical")
	if _, err := db.Exec(`
		INSERT INTO article_shares (public_id, article_id, created_by, snapshot, created_at)
		VALUES ($1, $2, $3, '{}'::jsonb, NOW())`, publicID, articleID, userID); err != nil {
		t.Fatalf("insert historical share: %v", err)
	}

	if err := testdb.ExecuteMigrationFile(db, "041_article_share_short_codes.sql"); err != nil {
		t.Fatalf("apply 041: %v", err)
	}
	if err := testdb.ExecuteMigrationFile(db, "041_article_share_short_codes.sql"); err != nil {
		t.Fatalf("reapply 041: %v", err)
	}

	var shortCode *string
	if err := db.QueryRow(`SELECT short_code FROM article_shares WHERE public_id = $1`, publicID).Scan(&shortCode); err != nil {
		t.Fatalf("historical short_code: %v", err)
	}
	if shortCode != nil {
		t.Fatalf("historical short_code = %q, want NULL", *shortCode)
	}

	for _, code := range []string{"Aa0000000000", "aa0000000000"} {
		if _, err := db.Exec(`
			INSERT INTO article_shares (public_id, article_id, created_by, snapshot, short_code)
			VALUES ($1, $2, $3, '{}'::jsonb, $4)`, deterministicArticleSharePublicID(code), articleID, userID, code); err != nil {
			t.Fatalf("insert short_code %q: %v", code, err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO article_shares (public_id, article_id, created_by, snapshot, short_code)
		VALUES ($1, $2, $3, '{}'::jsonb, 'Aa0000000000')`, deterministicArticleSharePublicID("duplicate"), articleID, userID); err == nil {
		t.Fatal("duplicate short_code was accepted")
	}

	for _, code := range []string{"short", "Aa00000000000", "Aa00000000_0", "Aa00000000-0"} {
		if _, err := db.Exec(`
			INSERT INTO article_shares (public_id, article_id, created_by, snapshot, short_code)
			VALUES ($1, $2, $3, '{}'::jsonb, $4)`, deterministicArticleSharePublicID(code), articleID, userID, code); err == nil {
			t.Errorf("invalid short_code %q was accepted", code)
		}
	}
	_ = feedID
}

func insertArticleShareMigrationFixture(t *testing.T, db *sql.DB) (userID, feedID, articleID int) {
	t.Helper()
	if err := db.QueryRow(`INSERT INTO users(username, password_hash) VALUES ('share-short-code', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url, title, owner_id) VALUES ('https://short-code.example/feed', 'Short code', $1) RETURNING id`, userID).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id, title, url, content) VALUES ($1, 'Article', 'https://short-code.example/article', 'body') RETURNING id`, feedID).Scan(&articleID); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return userID, feedID, articleID
}

func deterministicArticleSharePublicID(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:16])
}
