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

func NewFeedHandler(repo *repository.FeedRepository, articleRepo *repository.ArticleRepository) *FeedHandler {
	return &FeedHandler{
		repo:           repo,
		articleRepo:    articleRepo,
		fetcher:        rss.NewFetcher(),
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

	if err := validatePublicURL(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := h.fetcher.Preview(ctx, req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法获取该地址: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *FeedHandler) Create(c *gin.Context) {
	var req model.AddFeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
				FeedID:  feed.ID,
				Title:   item.Title,
				URL:     item.Link,
				Content: content,
			}
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
		if exists {
			h.articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, publishedTime(item.PublishedParsed, item.UpdatedParsed))
			continue
		}

		content := item.Description
		if content == "" {
			content = item.Content
		}

		if item.Link != "" {
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
