package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

type ContentHandler struct {
	articleRepo    *repository.ArticleRepository
	feedRepo       *repository.FeedRepository
	fetcher        *rss.Fetcher
	contentFetcher *rss.ContentFetcher
}

func NewContentHandler(articleRepo *repository.ArticleRepository, feedRepo *repository.FeedRepository, fetcher *rss.Fetcher) *ContentHandler {
	return &ContentHandler{
		articleRepo:    articleRepo,
		feedRepo:       feedRepo,
		fetcher:        fetcher,
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
	wc, rm := rss.ComputeMetrics(content)
	if err := h.articleRepo.UpdateContent(id, content, wc, rm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Best-effort: re-read the parent feed's RSS to backfill media_url for
	// audio/video enclosures. Failures are logged and swallowed — the content
	// update is what the user really asked for.
	h.tryBackfillMedia(c.Request.Context(), article)

	c.JSON(http.StatusOK, gin.H{"content": content})
}

// tryBackfillMedia re-fetches the article's parent feed (ignoring HTTP cache
// headers) and, if an audio/video enclosure exists for this article's URL,
// fills the media_* columns via UpdateMediaIfNull. All failures are logged
// and swallowed — this is opportunistic.
func (h *ContentHandler) tryBackfillMedia(ctx context.Context, article *model.Article) {
	if h.feedRepo == nil || h.fetcher == nil {
		return
	}
	feed, err := h.feedRepo.GetByID(article.FeedID)
	if err != nil || feed == nil {
		return
	}
	// Only RSS-shaped feeds carry enclosures.
	if feed.FeedType != "rss" && feed.FeedType != "podcast" && feed.FeedType != "youtube" {
		return
	}
	if feed.URL == "" {
		return
	}
	// Pass empty etag/lastModified to force a true fresh re-read.
	result, err := h.fetcher.Fetch(ctx, feed.URL, "", "")
	if err != nil || result == nil || result.Feed == nil {
		log.Printf("re-fetch RSS for media backfill failed feed=%d: %v", feed.ID, err)
		return
	}
	for _, item := range result.Feed.Items {
		if item == nil || item.Link == "" || item.Link != article.URL {
			continue
		}
		mi := rss.ExtractMedia(item)
		if mi == nil {
			return
		}
		if err := h.articleRepo.UpdateMediaIfNull(feed.ID, article.URL, mi.URL, mi.Type, mi.Duration); err != nil {
			log.Printf("UpdateMediaIfNull failed article=%d feed=%d: %v", article.ID, feed.ID, err)
		}
		return
	}
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
