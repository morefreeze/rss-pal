package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

type FeedHandler struct {
	repo           *repository.FeedRepository
	articleRepo    *repository.ArticleRepository
	fetcher        *rss.Fetcher
	contentFetcher *rss.ContentFetcher
}

func NewFeedHandler(repo *repository.FeedRepository, articleRepo *repository.ArticleRepository, rsshubBase string) *FeedHandler {
	return &FeedHandler{
		repo:           repo,
		articleRepo:    articleRepo,
		fetcher:        rss.NewFetcher(rsshubBase),
		contentFetcher: rss.NewContentFetcher(),
	}
}

func (h *FeedHandler) GetAll(c *gin.Context) {
	userID := getUserID(c)
	feeds, err := h.repo.GetVisibleByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, feeds)
}

func (h *FeedHandler) GetByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	feed, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}
	c.JSON(http.StatusOK, feed)
}

// Preview fetches a URL and returns up to 10 articles without saving anything.
func (h *FeedHandler) Preview(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url required"})
		return
	}

	// Auto-add https:// if no scheme provided
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		req.URL = "https://" + req.URL
	}

	if err := validatePublicURL(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	result, err := h.fetcher.Preview(ctx, req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": classifyPreviewError(err)})
		return
	}

	// Make sure actual_url is set (used by frontend to confirm the add)
	if result.ActualURL == "" {
		result.ActualURL = req.URL
	}
	c.JSON(http.StatusOK, result)
}

func (h *FeedHandler) Create(c *gin.Context) {
	var req model.AddFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		req.URL = "https://" + req.URL
	}

	feedType := req.FeedType
	if feedType == "" {
		feedType = "rss"
	}

	feed := &model.Feed{
		URL:              req.URL,
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          getOwnerID(c),
		FeedType:         feedType,
	}

	if err := h.repo.Create(feed); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "该订阅地址已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, feed)
}

func (h *FeedHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	existing, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if !isAdmin(c) && (existing.OwnerID == nil || *existing.OwnerID != getUserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	var feed model.Feed
	if err := c.ShouldBindJSON(&feed); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	feed.ID = id
	if err := h.repo.Update(&feed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, feed)
}

func (h *FeedHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	feed, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if !isAdmin(c) && (feed.OwnerID == nil || *feed.OwnerID != getUserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *FeedHandler) FetchNow(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	feed, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if !isAdmin(c) && (feed.OwnerID == nil || *feed.OwnerID != getUserID(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	log.Printf("Manual fetch triggered for feed: %s", feed.URL)

	// HTML feeds use scraping instead of RSS parsing
	if feed.FeedType == "html" {
		htmlFeed, err := h.fetcher.FetchHTML(c.Request.Context(), feed.URL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "fetch failed: " + err.Error()})
			return
		}
		if err := h.repo.UpdateFetchInfo(feed.ID, "", "", time.Now()); err != nil {
			log.Printf("Failed to update feed info: %v", err)
		}
		if htmlFeed.Title != "" {
			_ = h.repo.UpdateTitle(feed.ID, htmlFeed.Title)
		}
		newCount := 0
		for _, item := range htmlFeed.Items {
			if item.Link == "" {
				continue
			}
			exists, _ := h.articleRepo.Exists(feed.ID, item.Link)
			if exists {
				continue
			}
			content, _ := h.contentFetcher.FetchContent(c.Request.Context(), item.Link)
			article := &model.Article{
				FeedID:      feed.ID,
				Title:       item.Title,
				URL:         item.Link,
				Content:     content,
				PublishedAt: publishedTime(item.PublishedParsed, item.UpdatedParsed),
			}
			article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
			if err := h.articleRepo.Create(article); err != nil {
				log.Printf("Failed to create article: %v", err)
			} else {
				newCount++
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"message":      "fetch completed",
			"new_articles": newCount,
			"feed_title":   htmlFeed.Title,
		})
		return
	}

	result, err := h.fetcher.Fetch(c.Request.Context(), feed.URL, feed.ETag, feed.LastModified)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fetch failed: " + err.Error()})
		return
	}

	if result == nil {
		c.JSON(http.StatusOK, gin.H{"message": "not modified", "new_articles": 0})
		return
	}

	if err := h.repo.UpdateFetchInfo(feed.ID, result.ETag, result.LastModified, time.Now()); err != nil {
		log.Printf("Failed to update feed info: %v", err)
	}
	if result.Feed != nil && result.Feed.Title != "" {
		if err := h.repo.UpdateTitle(feed.ID, result.Feed.Title); err != nil {
			log.Printf("Failed to update feed title: %v", err)
		}
	}

	newCount := 0
	for _, item := range result.Feed.Items {
		if newCount >= 10 {
			break
		}

		exists, _ := h.articleRepo.Exists(feed.ID, item.Link)
		mediaInfo := rss.ExtractVideoMedia(item.Link)
		if mediaInfo == nil {
			mediaInfo = rss.ExtractMedia(item)
		}
		if exists {
			h.articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, publishedTime(item.PublishedParsed, item.UpdatedParsed))
			if mediaInfo != nil {
				if err := h.articleRepo.UpdateMediaIfNull(feed.ID, item.Link, mediaInfo.URL, mediaInfo.Type, mediaInfo.Duration); err != nil {
					log.Printf("Failed to backfill media for %s: %v", item.Link, err)
				}
			}
			continue
		}

		content := rss.StripHTML(item.Description)
		if content == "" {
			content = rss.StripHTML(item.Content)
		}

		skipDeepFetch := feed.FeedType == "youtube" || feed.FeedType == "podcast"
		if !skipDeepFetch && item.Link != "" {
			fullContent, err := h.contentFetcher.FetchContent(c.Request.Context(), item.Link)
			if err == nil && len(fullContent) > len(content) {
				content = fullContent
			}
		}

		article := &model.Article{
			FeedID:      feed.ID,
			Title:       item.Title,
			URL:         item.Link,
			Content:     content,
			PublishedAt: publishedTime(item.PublishedParsed, item.UpdatedParsed),
		}
		article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
		if mediaInfo != nil {
			article.MediaURL = mediaInfo.URL
			article.MediaType = mediaInfo.Type
			article.MediaDurationSeconds = mediaInfo.Duration
		}

		if err := h.articleRepo.Create(article); err != nil {
			log.Printf("Failed to create article: %v", err)
		} else {
			newCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "fetch completed",
		"new_articles": newCount,
		"feed_title":  result.Feed.Title,
	})
}

func publishedTime(published, updated *time.Time) *time.Time {
	if published != nil {
		return published
	}
	return updated
}

func (h *FeedHandler) ExportOPML(c *gin.Context) {
	userID := getUserID(c)
	feeds, err := h.repo.GetVisibleByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<opml version="2.0">` + "\n")
	sb.WriteString(`  <head><title>RSS Pal Subscriptions</title></head>` + "\n")
	sb.WriteString(`  <body>` + "\n")
	for _, feed := range feeds {
		title := feed.Title
		if title == "" {
			title = feed.URL
		}
		feedType := "rss"
		if feed.FeedType == "html" {
			feedType = "html"
		}
		sb.WriteString(`    <outline type="` + feedType + `" text="` + xmlEscape(title) + `" title="` + xmlEscape(title) + `" xmlUrl="` + xmlEscape(feed.URL) + `"/>` + "\n")
	}
	sb.WriteString(`  </body>` + "\n")
	sb.WriteString(`</opml>` + "\n")

	c.Header("Content-Type", "text/xml; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=rss-pal-subscriptions.opml")
	c.String(http.StatusOK, sb.String())
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// UpdateStatus PATCH /api/feeds/:id/status
func (h *FeedHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateStatus(id, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateWeight PATCH /api/feeds/:id/weight
func (h *FeedHandler) UpdateWeight(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		PriorityWeight float64 `json:"priority_weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateWeight(id, req.PriorityWeight); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// classifyPreviewError turns an internal fetch/parse error into a user-facing
// Chinese message. The fetcher emits errors like "server returned 429" or
// "failed to fetch URL: ... context deadline exceeded" which are unfriendly
// to non-engineers. Rate-limit and transient upstream failures are surfaced
// distinctly so users know to retry.
func classifyPreviewError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "server returned 429"):
		return "源站限流，请稍后重试（HTTP 429）"
	case strings.Contains(s, "server returned 503"):
		return "源站暂时不可用，请稍后重试（HTTP 503）"
	case strings.Contains(s, "server returned 403"):
		return "源站拒绝访问（HTTP 403），可能需要登录或被反爬拦截"
	case strings.Contains(s, "server returned 404"):
		return "URL 不存在（HTTP 404），请检查地址"
	case strings.Contains(s, "server returned 5"):
		return "源站异常（" + extractStatusFromErr(s) + "），请稍后重试"
	case strings.Contains(s, "context deadline exceeded") || strings.Contains(s, "Timeout"):
		return "请求超时，请稍后重试"
	case strings.Contains(s, "no such host"):
		return "无法解析域名，请检查 URL 拼写"
	case strings.Contains(s, "connection refused"):
		return "无法连接到该地址"
	case strings.Contains(s, "failed to parse"):
		return "无法解析该地址的内容，可能不是有效的 RSS/Atom"
	default:
		return "无法获取该地址: " + s
	}
}

// extractStatusFromErr pulls "HTTP NNN" from a "server returned NNN" error.
func extractStatusFromErr(s string) string {
	const prefix = "server returned "
	i := strings.Index(s, prefix)
	if i < 0 {
		return "HTTP 5xx"
	}
	rest := s[i+len(prefix):]
	if len(rest) >= 3 {
		return "HTTP " + rest[:3]
	}
	return "HTTP 5xx"
}

// validatePublicURL blocks SSRF by rejecting non-HTTP(S) schemes and private/loopback IPs.
func validatePublicURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https URLs are supported")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid URL: missing host")
	}

	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("private or internal addresses are not allowed")
		}
	}

	// Block common internal hostnames
	lower := strings.ToLower(host)
	blocked := []string{"localhost", "metadata.google.internal", "169.254.169.254"}
	for _, b := range blocked {
		if lower == b {
			return fmt.Errorf("private or internal addresses are not allowed")
		}
	}

	return nil
}
