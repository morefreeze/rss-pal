package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

type ShareRepository struct {
	db Querier
}

func NewShareRepository(db *sql.DB) *ShareRepository {
	return &ShareRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *ShareRepository) WithCtx(c ctxkey.CtxGetter) *ShareRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &ShareRepository{db: q}
		}
	}
	return r
}

// SnapshotFromArticle selects only fields that are safe to expose on the
// unauthenticated share surface.
func SnapshotFromArticle(a *model.Article, now time.Time) model.ArticleShareSnapshot {
	return model.ArticleShareSnapshot{
		Title:                a.Title,
		URL:                  a.URL,
		FeedTitle:            a.FeedTitle,
		PublishedAt:          a.PublishedAt,
		WordCount:            a.WordCount,
		ReadingMinutes:       a.ReadingMinutes,
		SummaryBrief:         a.SummaryBrief,
		SummaryDetailed:      a.SummaryDetailed,
		Content:              a.Content,
		MediaURL:             a.MediaURL,
		MediaType:            a.MediaType,
		MediaDurationSeconds: a.MediaDurationSeconds,
		ImageDimensions:      a.ImageDimensions,
		SnapshottedAt:        now,
	}
}

func (r *ShareRepository) Create(article *model.Article, createdBy int, publicID string, expiresAt *time.Time, now time.Time) (*model.ArticleShare, error) {
	snapshotJSON, err := json.Marshal(SnapshotFromArticle(article, now))
	if err != nil {
		return nil, err
	}
	return scanArticleShare(r.db.QueryRow(`
		INSERT INTO article_shares (
			public_id, article_id, created_by, snapshot_version, snapshot, expires_at, created_at
		) VALUES ($1, $2, $3, 1, $4, $5, $6)
		RETURNING public_id, article_id, created_by, snapshot_version, snapshot,
		          expires_at, revoked_at, legacy_token_digest, created_at`,
		publicID, article.ID, createdBy, snapshotJSON, expiresAt, now,
	))
}

// List returns all shares for the article and owner, including expired and
// revoked rows so the management UI can display their lifecycle state.
func (r *ShareRepository) List(articleID, createdBy int, _ time.Time) ([]model.ArticleShare, error) {
	rows, err := r.db.Query(`
		SELECT public_id, article_id, created_by, snapshot_version, snapshot,
		       expires_at, revoked_at, legacy_token_digest, created_at
		  FROM article_shares
		 WHERE article_id = $1 AND created_by = $2
		 ORDER BY created_at DESC, public_id DESC`, articleID, createdBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares := make([]model.ArticleShare, 0)
	for rows.Next() {
		share, err := scanArticleShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, *share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *ShareRepository) Revoke(publicID string, articleID, createdBy int, now time.Time) (*model.ArticleShare, error) {
	return nilOnNoRows(scanArticleShare(r.db.QueryRow(`
		UPDATE article_shares
		   SET revoked_at = COALESCE(revoked_at, $4)
		 WHERE public_id = $1 AND article_id = $2 AND created_by = $3
		RETURNING public_id, article_id, created_by, snapshot_version, snapshot,
		          expires_at, revoked_at, legacy_token_digest, created_at`,
		publicID, articleID, createdBy, now,
	)))
}

func (r *ShareRepository) GetActiveByPublicID(publicID string, now time.Time) (*model.ArticleShare, error) {
	return nilOnNoRows(scanArticleShare(r.db.QueryRow(`
		SELECT public_id, article_id, created_by, snapshot_version, snapshot,
		       expires_at, revoked_at, legacy_token_digest, created_at
		  FROM article_shares
		 WHERE public_id = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > $2)`, publicID, now)))
}

func (r *ShareRepository) GetActiveByLegacyDigest(digest string, now time.Time) (*model.ArticleShare, error) {
	return nilOnNoRows(scanArticleShare(r.db.QueryRow(`
		SELECT public_id, article_id, created_by, snapshot_version, snapshot,
		       expires_at, revoked_at, legacy_token_digest, created_at
		  FROM article_shares
		 WHERE legacy_token_digest = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > $2)`, digest, now)))
}

type shareScanner interface {
	Scan(dest ...interface{}) error
}

func scanArticleShare(scanner shareScanner) (*model.ArticleShare, error) {
	var share model.ArticleShare
	var snapshotJSON []byte
	var expiresAt, revokedAt sql.NullTime
	var legacyTokenDigest sql.NullString
	if err := scanner.Scan(
		&share.PublicID,
		&share.ArticleID,
		&share.CreatedBy,
		&share.SnapshotVersion,
		&snapshotJSON,
		&expiresAt,
		&revokedAt,
		&legacyTokenDigest,
		&share.CreatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshotJSON, &share.Snapshot); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		share.ExpiresAt = &expiresAt.Time
	}
	if revokedAt.Valid {
		share.RevokedAt = &revokedAt.Time
	}
	if legacyTokenDigest.Valid {
		share.LegacyTokenDigest = &legacyTokenDigest.String
	}
	return &share, nil
}

func nilOnNoRows(share *model.ArticleShare, err error) (*model.ArticleShare, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return share, err
}
