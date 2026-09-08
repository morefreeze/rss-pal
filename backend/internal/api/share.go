package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/sharetoken"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shares   *repository.ShareRepository
	articles *repository.ArticleRepository
	signer   *sharetoken.Signer
	now      func() time.Time
	images   *ArticleImageHandler
}

func NewShareHandler(
	shares *repository.ShareRepository,
	articles *repository.ArticleRepository,
	signer *sharetoken.Signer,
	images *ArticleImageHandler,
	now func() time.Time,
) *ShareHandler {
	if now == nil {
		now = time.Now
	}
	return &ShareHandler{shares: shares, articles: articles, signer: signer, now: now, images: images}
}

type shareManagementResponse struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
	Legacy    bool       `json:"legacy"`
}

func (h *ShareHandler) Create(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}
	var request struct {
		ExpiresAt *string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	now := h.now()
	var expiresAt *time.Time
	if request.ExpiresAt != nil {
		parsed, err := time.Parse(time.RFC3339, *request.ExpiresAt)
		if err != nil || !parsed.After(now) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be a future RFC3339 timestamp"})
			return
		}
		expiresAt = &parsed
	}

	userID := getUserID(c)
	article, ok := h.visibleArticle(c, articleID, userID)
	if !ok {
		return
	}
	if (article.ProcessingState != "" && article.ProcessingState != "ready") || strings.TrimSpace(article.Content) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "article is not ready to share"})
		return
	}

	publicID, err := sharetoken.NewPublicID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	row, err := h.shares.WithCtx(c).Create(article, userID, publicID, expiresAt, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, h.managementResponse(row, now))
}

func (h *ShareHandler) List(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}
	userID := getUserID(c)
	if _, ok := h.visibleArticle(c, articleID, userID); !ok {
		return
	}

	now := h.now()
	rows, err := h.shares.WithCtx(c).List(articleID, userID, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	response := make([]shareManagementResponse, 0, len(rows))
	for i := range rows {
		response = append(response, h.managementResponse(&rows[i], now))
	}
	c.JSON(http.StatusOK, response)
}

func (h *ShareHandler) Revoke(c *gin.Context) {
	articleID, ok := parseShareArticleID(c)
	if !ok {
		return
	}
	userID := getUserID(c)
	if _, ok := h.visibleArticle(c, articleID, userID); !ok {
		return
	}

	now := h.now()
	row, err := h.shares.WithCtx(c).Revoke(c.Param("share_id"), articleID, userID, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	c.JSON(http.StatusOK, h.managementResponse(row, now))
}

func (h *ShareHandler) visibleArticle(c *gin.Context, articleID, userID int) (*model.Article, bool) {
	article, err := h.articles.WithCtx(c).GetByID(articleID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return nil, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	return article, true
}

func (h *ShareHandler) managementResponse(row *model.ArticleShare, now time.Time) shareManagementResponse {
	status := "active"
	if row.RevokedAt != nil {
		status = "revoked"
	} else if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
		status = "expired"
	}
	return shareManagementResponse{
		ID:        row.PublicID,
		URL:       "/share/" + h.signer.Sign(row.PublicID),
		CreatedAt: row.CreatedAt,
		ExpiresAt: row.ExpiresAt,
		Status:    status,
		Legacy:    row.LegacyTokenDigest != nil,
	}
}

func parseShareArticleID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}
