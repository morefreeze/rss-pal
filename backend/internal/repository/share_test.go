package repository

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"github.com/lib/pq"
)

type shareRepoFixture struct {
	db      *sql.DB
	repo    *ShareRepository
	userA   int
	userB   int
	article *model.Article
}

type errShareScanner struct {
	err error
}

func (s errShareScanner) Scan(...interface{}) error {
	return s.err
}

func TestShareRepositoryScannerPreservesNoRowsForCreateContract(t *testing.T) {
	got, err := scanArticleShare(errShareScanner{err: sql.ErrNoRows})
	if got != nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("got=%+v err=%v, want nil, sql.ErrNoRows", got, err)
	}
}

func newShareRepoFixture(t *testing.T) *shareRepoFixture {
	t.Helper()
	db, cleanup := testdb.New(t)
	t.Cleanup(cleanup)

	f := &shareRepoFixture{db: db, repo: NewShareRepository(db)}
	for target, username := range map[*int]string{&f.userA: "share-a", &f.userB: "share-b"} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, username).Scan(target); err != nil {
			t.Fatal(err)
		}
	}

	var feedID int
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://feed.example/rss?token=feed-secret','Feed A',$1) RETURNING id`, f.userA).Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	publishedAt := time.Date(2026, 9, 7, 8, 30, 0, 0, time.UTC)
	f.article = &model.Article{
		FeedID:               feedID,
		FeedTitle:            "Feed A",
		Title:                "Original",
		URL:                  "https://article.example/public-post",
		Content:              "Body mentions feed_id and editor_note as ordinary text",
		PublishedAt:          &publishedAt,
		SummaryBrief:         "Brief",
		SummaryDetailed:      "Detail",
		WordCount:            10,
		ReadingMinutes:       2,
		IsRead:               true,
		ProcessingState:      "ready",
		EditorNote:           "editor-note-secret",
		MediaURL:             "https://cdn.example/audio.mp3",
		MediaType:            "audio/mpeg",
		MediaDurationSeconds: 125,
		ImageDimensions: map[string][2]int{
			"https://img.example/hero.jpg": {1200, 800},
		},
	}
	imageDimensions, err := json.Marshal(f.article.ImageDimensions)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO articles(
			feed_id,title,url,content,published_at,summary_brief,summary_detailed,
			word_count,reading_minutes,processing_state,editor_note,
			media_url,media_type,media_duration_seconds,image_dimensions
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'ready',$10,$11,$12,$13,$14)
		RETURNING id`,
		feedID, f.article.Title, f.article.URL, f.article.Content, f.article.PublishedAt,
		f.article.SummaryBrief, f.article.SummaryDetailed, f.article.WordCount,
		f.article.ReadingMinutes, f.article.EditorNote, f.article.MediaURL,
		f.article.MediaType, f.article.MediaDurationSeconds, imageDimensions,
	).Scan(&f.article.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestShareRepositoryCreateListRevokeAndSnapshotImmutability(t *testing.T) {
	f := newShareRepoFixture(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	first, err := f.repo.Create(f.article, f.userA, "11111111111111111111111111111111", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	secondExpiry := now.Add(time.Hour)
	second, err := f.repo.Create(f.article, f.userA, "22222222222222222222222222222222", &secondExpiry, now)
	if err != nil || first.PublicID == second.PublicID {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	if _, err := f.db.Exec(`UPDATE articles SET title='Changed', content='Changed body' WHERE id=$1`, f.article.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.GetActiveByPublicID(first.PublicID, now)
	if err != nil || got == nil || got.Snapshot.Title != "Original" || got.Snapshot.Content != "Body mentions feed_id and editor_note as ordinary text" {
		t.Fatalf("got=%+v err=%v", got, err)
	}

	wantSnapshot := SnapshotFromArticle(f.article, now)
	if !reflect.DeepEqual(got.Snapshot, wantSnapshot) {
		t.Fatalf("snapshot mismatch:\n got=%+v\nwant=%+v", got.Snapshot, wantSnapshot)
	}
	assertWireSafeSnapshot(t, f.db, first.PublicID)

	rows, err := f.repo.List(f.article.ID, f.userA, now)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}

	revoked, err := f.repo.Revoke(first.PublicID, f.article.ID, f.userA, now)
	if err != nil || revoked == nil || revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(now) {
		t.Fatalf("revoked=%+v err=%v", revoked, err)
	}
	revokedAgain, err := f.repo.Revoke(first.PublicID, f.article.ID, f.userA, now.Add(time.Minute))
	if err != nil || revokedAgain == nil || revokedAgain.RevokedAt == nil || !revokedAgain.RevokedAt.Equal(now) {
		t.Fatalf("idempotent revoke=%+v err=%v", revokedAgain, err)
	}
	got, err = f.repo.GetActiveByPublicID(first.PublicID, now)
	if err != nil || got != nil {
		t.Fatalf("revoked got=%+v err=%v", got, err)
	}
}

func TestShareRepositoryOwnerFiltersListAndRevoke(t *testing.T) {
	f := newShareRepoFixture(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	row, err := f.repo.Create(f.article, f.userA, "33333333333333333333333333333333", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.repo.List(f.article.ID, f.userB, now)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if got, err := f.repo.Revoke(row.PublicID, f.article.ID, f.userB, now); err != nil || got != nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got, err := f.repo.GetActiveByPublicID(row.PublicID, now); err != nil || got == nil {
		t.Fatalf("other owner must not revoke: got=%+v err=%v", got, err)
	}
}

func TestShareRepositoryExpirationAndLegacyDigest(t *testing.T) {
	f := newShareRepoFixture(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	row, err := f.repo.Create(f.article, f.userA, "44444444444444444444444444444444", &expiresAt, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.GetActiveByPublicID(row.PublicID, expiresAt)
	if err != nil || got != nil {
		t.Fatalf("expired at exact boundary got=%+v err=%v", got, err)
	}

	legacyExpiry := now.Add(30 * 24 * time.Hour)
	digest := sharetoken.LegacyDigest("aB3dE6gH")
	if _, err := f.db.Exec(`UPDATE article_shares SET legacy_token_digest=$1, expires_at=$2 WHERE public_id=$3`, digest, legacyExpiry, row.PublicID); err != nil {
		t.Fatal(err)
	}
	got, err = f.repo.GetActiveByLegacyDigest(digest, now)
	if err != nil || got == nil {
		t.Fatalf("legacy got=%+v err=%v", got, err)
	}
	got, err = f.repo.GetActiveByLegacyDigest(digest, legacyExpiry)
	if err != nil || got != nil {
		t.Fatalf("expired legacy got=%+v err=%v", got, err)
	}
}

func TestShareRepositorySnapshotOmitsEmptyImageDimensions(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	for name, dimensions := range map[string]map[string][2]int{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(SnapshotFromArticle(&model.Article{
				Title:           "Public title",
				URL:             "https://article.example/public",
				Content:         "Public body",
				ImageDimensions: dimensions,
			}, now))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if _, ok := got["image_dimensions"]; ok {
				t.Fatalf("empty image dimensions must be omitted: %s", raw)
			}
		})
	}
}

func TestMigrations039CopiesLegacySharesAndCanBeReapplied(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "038_subscription_explore.sql")
	defer cleanup()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET TIME ZONE 'Asia/Shanghai'`); err != nil {
		t.Fatal(err)
	}
	executeMigration039DDLPrefix(t, db)

	var userID, feedID, articleID int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('legacy-share-owner','x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://legacy.example/feed','Legacy Feed',$1) RETURNING id`, userID).Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,published_at,summary_brief,summary_detailed,word_count,reading_minutes,media_url,media_type,media_duration_seconds,image_dimensions) VALUES($1,'Legacy','https://legacy.example/post','Legacy body','2026-09-01 12:34:56','Legacy brief','Legacy detail',20,3,'https://legacy.example/audio','audio/mpeg',60,'{"https://legacy.example/image":[640,480]}') RETURNING id`, feedID).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO share_tokens(article_id,token,created_by,created_at) VALUES($1,'aB3dE6gH',$2,'2026-09-01 10:00:00')`, articleID, userID); err != nil {
		t.Fatal(err)
	}

	if err := testdb.ExecuteMigrationFile(db, "039_article_shares.sql"); err != nil {
		t.Fatal(err)
	}
	if err := testdb.ExecuteMigrationFile(db, "039_article_shares.sql"); err != nil {
		t.Fatalf("reapply migration: %v", err)
	}

	var oldTableExists bool
	if err := db.QueryRow(`SELECT to_regclass('share_tokens') IS NOT NULL`).Scan(&oldTableExists); err != nil {
		t.Fatal(err)
	}
	if oldTableExists {
		t.Fatal("share_tokens table still exists")
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM article_shares`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migrated share count=%d, want 1", count)
	}

	var publicID, digest string
	var snapshotJSON []byte
	var createdAt, expiresAt time.Time
	if err := db.QueryRow(`SELECT public_id,legacy_token_digest,snapshot,created_at,expires_at FROM article_shares`).Scan(&publicID, &digest, &snapshotJSON, &createdAt, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if len(publicID) != 32 || digest != sharetoken.LegacyDigest("aB3dE6gH") {
		t.Fatalf("public_id=%q digest=%q", publicID, digest)
	}
	if !expiresAt.After(createdAt) {
		t.Fatalf("created_at=%s expires_at=%s", createdAt, expiresAt)
	}
	wantCreatedAt := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	if !createdAt.Equal(wantCreatedAt) {
		t.Fatalf("legacy created_at instant=%s, want %s", createdAt, wantCreatedAt)
	}
	var snapshot model.ArticleShareSnapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "Legacy" || snapshot.FeedTitle != "Legacy Feed" || snapshot.Content != "Legacy body" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	wantPublishedAt := time.Date(2026, 9, 1, 4, 34, 56, 0, time.UTC)
	if snapshot.PublishedAt == nil || !snapshot.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("legacy published_at=%v, want instant %s", snapshot.PublishedAt, wantPublishedAt)
	}
}

func TestMigration039RelocatesPollutedPGCryptoAndCopiesLegacyShare(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "038_subscription_explore.sql")
	defer cleanup()
	db.SetMaxOpenConns(1)
	if err := testdb.WithSchemaBootstrapLock(db, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		rolledBack := false
		defer func() {
			if !rolledBack {
				_ = tx.Rollback()
			}
		}()

		var schema string
		if err := tx.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
			return err
		}
		var extensionSchema string
		err = tx.QueryRow(`
			SELECT n.nspname
			  FROM pg_extension e
			  JOIN pg_namespace n ON n.oid = e.extnamespace
			 WHERE e.extname = 'pgcrypto'`).Scan(&extensionSchema)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.Exec(`CREATE EXTENSION pgcrypto WITH SCHEMA ` + pq.QuoteIdentifier(schema)); err != nil {
				return err
			}
		case err != nil:
			return err
		case extensionSchema != schema:
			if _, err := tx.Exec(`ALTER EXTENSION pgcrypto SET SCHEMA ` + pq.QuoteIdentifier(schema)); err != nil {
				return err
			}
		}

		var userID, feedID, articleID int
		if err := tx.QueryRow(`INSERT INTO users(username,password_hash) VALUES('polluted-share-owner','x') RETURNING id`).Scan(&userID); err != nil {
			return err
		}
		if err := tx.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://polluted.example/feed','Polluted Feed',$1) RETURNING id`, userID).Scan(&feedID); err != nil {
			return err
		}
		if err := tx.QueryRow(`INSERT INTO articles(feed_id,title,url,content) VALUES($1,'Polluted','https://polluted.example/post','body') RETURNING id`, feedID).Scan(&articleID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO share_tokens(article_id,token,created_by) VALUES($1,'pG3cR7yP',$2)`, articleID, userID); err != nil {
			return err
		}

		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "039_article_shares.sql"))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(raw)); err != nil {
			return fmt.Errorf("execute migration 039 from polluted pgcrypto schema: %w", err)
		}
		if err := tx.QueryRow(`
			SELECT n.nspname
			  FROM pg_extension e
			  JOIN pg_namespace n ON n.oid = e.extnamespace
			 WHERE e.extname = 'pgcrypto'`).Scan(&extensionSchema); err != nil {
			return err
		}
		if extensionSchema != "public" {
			return fmt.Errorf("pgcrypto schema=%q, want public", extensionSchema)
		}
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM article_shares WHERE legacy_token_digest=$1`, sharetoken.LegacyDigest("pG3cR7yP")).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("migrated legacy shares=%d, want 1", count)
		}
		if err := tx.Rollback(); err != nil {
			return err
		}
		rolledBack = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func executeMigration039DDLPrefix(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "039_article_shares.sql"))
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(string(raw), "-- Legacy share-token migration begins here.")
	if !ok {
		t.Fatal("migration 039 has no legacy-copy DO block")
	}
	if _, err := db.Exec(prefix); err != nil {
		t.Fatalf("execute migration 039 DDL prefix: %v", err)
	}
}

func assertWireSafeSnapshot(t *testing.T, db *sql.DB, publicID string) {
	t.Helper()
	var snapshotJSON []byte
	if err := db.QueryRow(`SELECT snapshot FROM article_shares WHERE public_id=$1`, publicID).Scan(&snapshotJSON); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(snapshotJSON, &got); err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(snapshotJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]struct{}{}
	for _, key := range []string{
		"created_by", "user_id", "feed_id", "editor_note", "processing_error", "is_read", "manual_tags",
	} {
		forbidden[key] = struct{}{}
	}
	var walk func(any, string)
	walk = func(value any, path string) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if _, private := forbidden[key]; private {
					t.Errorf("snapshot exposes private key %q at %s", key, path)
				}
				walk(child, path+"."+key)
			}
		case []any:
			for i, child := range value {
				walk(child, path+"["+strconv.Itoa(i)+"]")
			}
		}
	}
	walk(decoded, "$")
	wantKeys := map[string]struct{}{}
	for _, key := range []string{
		"title", "url", "feed_title", "published_at", "word_count", "reading_minutes",
		"summary_brief", "summary_detailed", "content", "media_url", "media_type",
		"media_duration_seconds", "image_dimensions", "snapshotted_at",
	} {
		wantKeys[key] = struct{}{}
	}
	gotKeys := make(map[string]struct{}, len(got))
	for key := range got {
		gotKeys[key] = struct{}{}
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("snapshot keys=%v, want exact allowlist=%v: %s", gotKeys, wantKeys, snapshotJSON)
	}
	var articleURL string
	if err := json.Unmarshal(got["url"], &articleURL); err != nil {
		t.Fatal(err)
	}
	if articleURL != "https://article.example/public-post" {
		t.Errorf("snapshot article url=%q", articleURL)
	}
	for _, sentinel := range [][]byte{
		[]byte("https://feed.example/rss?token=feed-secret"),
		[]byte("feed-secret"),
		[]byte("editor-note-secret"),
	} {
		if bytes.Contains(snapshotJSON, sentinel) {
			t.Errorf("snapshot leaked private sentinel %q: %s", sentinel, snapshotJSON)
		}
	}
}
