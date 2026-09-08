package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
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

	now := h.now()
	expiresAt, ok := parseShareExpiry(c, now)
	if !ok {
		return
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
		shareInternalError(c, "generate public ID", err)
		return
	}
	row, err := h.shares.WithCtx(c).Create(article, userID, publicID, expiresAt, now)
	if err != nil {
		shareInternalError(c, "create", err)
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
		shareInternalError(c, "list", err)
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
	if !validSharePublicID(c.Param("share_id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}

	now := h.now()
	row, err := h.shares.WithCtx(c).Revoke(c.Param("share_id"), articleID, userID, now)
	if err != nil {
		shareInternalError(c, "revoke", err)
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
		shareInternalError(c, "authorize article", err)
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

func parseShareExpiry(c *gin.Context, now time.Time) (*time.Time, bool) {
	var request struct {
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return nil, false
	}

	raw := bytes.TrimSpace(request.ExpiresAt)
	if len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at is required"})
		return nil, false
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, true
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be null or a future RFC3339 timestamp"})
		return nil, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || !parsed.After(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be null or a future RFC3339 timestamp"})
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func validSharePublicID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !((id[i] >= '0' && id[i] <= '9') || (id[i] >= 'a' && id[i] <= 'f')) {
			return false
		}
	}
	return true
}

func shareInternalError(c *gin.Context, operation string, err error) {
	log.Printf("share %s failed: %v", operation, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
