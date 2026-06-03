package api

import (
	"net/http"
	"strconv"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type ShareHandler struct {
	shareRepo   *repository.ShareRepository
	articleRepo *repository.ArticleRepository
}

func NewShareHandler(shareRepo *repository.ShareRepository, articleRepo *repository.ArticleRepository) *ShareHandler {
	return &ShareHandler{shareRepo: shareRepo, articleRepo: articleRepo}
}

// Create POST /api/articles/:id/share — 生成/获取 share token
func (h *ShareHandler) Create(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	userID := getUserID(c)
	st, err := h.shareRepo.WithCtx(c).GetOrCreate(id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": st.Token,
		"url":   "/share/" + st.Token,
	})
}

// GetByToken GET /api/share/:token — 公开接口（无需认证）
func (h *ShareHandler) GetByToken(c *gin.Context) {
	token := c.Param("token")

	article, err := h.shareRepo.WithCtx(c).GetArticleByToken(token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if article == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share token not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"title":            article.Title,
		"url":              article.URL,
		"summary_brief":    article.SummaryBrief,
		"summary_detailed": article.SummaryDetailed,
		"published_at":     article.PublishedAt,
	})
}
