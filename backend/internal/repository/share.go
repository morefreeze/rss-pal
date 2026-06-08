package repository

import (
	"database/sql"
	"math/rand"

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

// GetOrCreate 创建 share token（如果该 article 已有 token，返回已有的）
func (r *ShareRepository) GetOrCreate(articleID, createdBy int) (*model.ShareToken, error) {
	// 先查是否已有
	st := &model.ShareToken{}
	err := r.db.QueryRow(
		`SELECT id, article_id, token, created_by, created_at FROM share_tokens WHERE article_id = $1`,
		articleID,
	).Scan(&st.ID, &st.ArticleID, &st.Token, &st.CreatedBy, &st.CreatedAt)
	if err == nil {
		return st, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// 生成8位随机字母数字 token
	token := generateShareToken(8)

	err = r.db.QueryRow(
		`INSERT INTO share_tokens (article_id, token, created_by) VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		articleID, token, createdBy,
	).Scan(&st.ID, &st.CreatedAt)
	if err != nil {
		return nil, err
	}

	st.ArticleID = articleID
	st.Token = token
	st.CreatedBy = createdBy
	return st, nil
}

// GetArticleByToken 通过 token 获取 article
func (r *ShareRepository) GetArticleByToken(token string) (*model.Article, error) {
	var a model.Article
	var content, summaryBrief, summaryDetailed sql.NullString
	err := r.db.QueryRow(
		`SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		 FROM articles a
		 JOIN share_tokens st ON a.id = st.article_id
		 WHERE st.token = $1`,
		token,
	).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	return &a, nil
}

func generateShareToken(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
