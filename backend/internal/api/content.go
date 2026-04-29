package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

type ContentHandler struct {
	articleRepo    *repository.ArticleRepository
	contentFetcher *rss.ContentFetcher
}

func NewContentHandler(articleRepo *repository.ArticleRepository) *ContentHandler {
	return &ContentHandler{
		articleRepo:    articleRepo,
		contentFetcher: rss.NewContentFetcher(),
	}
}

func (h *ContentHandler) FetchContent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	article, err := h.articleRepo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	if article.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "article has no URL"})
		return
	}

	content, err := h.contentFetcher.FetchContent(c.Request.Context(), article.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if content == "" {
		c.JSON(http.StatusOK, gin.H{"content": article.Content, "message": "no content found"})
		return
	}

	// Update article content
	if err := h.articleRepo.UpdateContent(id, content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": content})
}

func (h *ContentHandler) ExportMarkdown(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	article, err := h.articleRepo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", article.Title))
	if article.PublishedAt != nil {
		sb.WriteString(fmt.Sprintf("> 发布时间：%s\n", article.PublishedAt.Format("2006-01-02 15:04")))
	}
	sb.WriteString(fmt.Sprintf("> 来源：%s\n\n", article.URL))

	if article.SummaryBrief != "" {
		sb.WriteString("## 要点摘要\n\n")
		sb.WriteString(article.SummaryBrief)
		sb.WriteString("\n\n")
	}

	if article.SummaryDetailed != "" {
		sb.WriteString("## 详细总结\n\n")
		sb.WriteString(article.SummaryDetailed)
		sb.WriteString("\n\n")
	}

	if article.Content != "" {
		sb.WriteString("---\n\n## 正文\n\n")
		sb.WriteString(article.Content)
		sb.WriteString("\n")
	}

	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.String(http.StatusOK, sb.String())
}
