package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *ArticleHandler) CaptureDrafts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid article id"})
		return
	}
	drafts, fetched, err := h.articleRepo.WithCtx(c).ListCaptureDrafts(getUserID(c), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": "article not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "无法读取待抓取列表"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"drafts": drafts, "fetched_link_urls": fetched})
}

func (h *ArticleHandler) DeleteCaptureDrafts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid article id"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024)
	var req struct {
		URLs []string `json:"urls"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.URLs) == 0 || len(req.URLs) > 100 {
		c.JSON(400, gin.H{"error": "invalid draft URLs"})
		return
	}
	err = h.articleRepo.WithCtx(c).DeleteCaptureDrafts(getUserID(c), id, req.URLs)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": "article not found"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "无法移除待抓取链接"})
		return
	}
	jsonAfterCommit(c, http.StatusNoContent, nil)
}
