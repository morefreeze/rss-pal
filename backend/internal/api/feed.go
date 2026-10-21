package api

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
)

type feedContentFetcher interface {
	FetchContent(context.Context, string) (string, error)
	FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error)
	FetchHTMLDocument(context.Context, string) (*goquery.Document, error)
}

type FeedHandler struct {
	repo           *repository.FeedRepository
	articleRepo    *repository.ArticleRepository
	fetcher        *rss.Fetcher
	contentFetcher feedContentFetcher
	backup         *backup.Runner // nil when backup is disabled
}

func NewFeedHandler(repo *repository.FeedRepository, articleRepo *repository.ArticleRepository, rsshubBase string) *FeedHandler {
	return &FeedHandler{
		repo:           repo,
		articleRepo:    articleRepo,
		fetcher:        rss.NewPublicFetcher(rsshubBase),
		contentFetcher: rss.NewContentFetcher(),
	}
}

// WithBackupRunner wires a backup runner so add/delete trigger a debounced
// snapshot. Pass nil to disable.
func (h *FeedHandler) WithBackupRunner(r *backup.Runner) *FeedHandler {
	h.backup = r
	return h
}

func (h *FeedHandler) GetAll(c *gin.Context) {
	userID := getUserID(c)
	feeds, err := h.repo.WithCtx(c).GetVisibleByUser(userID)
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

	feed, err := h.repo.WithCtx(c).GetByID(id)
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

	var err error
	req.URL, err = normalizeFeedURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL"})
		return
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

	var err error
	req.URL, err = normalizeFeedURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL"})
		return
	}

	feedType := req.FeedType
	if feedType == "" {
		feedType = "rss"
	}

	feed := &model.Feed{
		URL:              req.URL,
		Title:            strings.TrimSpace(req.Title),
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          getOwnerID(c),
		FeedType:         feedType,
		ExpandLinks:      req.ExpandLinks,
	}

	if err := h.repo.WithCtx(c).Create(feed); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "该订阅地址已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.backup != nil {
		h.backup.TriggerAsync()
	}
	c.JSON(http.StatusCreated, feed)
}

func (h *FeedHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	feedRepo := h.repo.WithCtx(c)
	existing, err := feedRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if existing.OwnerID == nil || *existing.OwnerID != getUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	var feed model.Feed
	if err := c.ShouldBindJSON(&feed); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	feed.ID = id
	if err := feedRepo.Update(&feed); err != nil {
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

	feedRepo := h.repo.WithCtx(c)
	feed, err := feedRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if feed.OwnerID == nil || *feed.OwnerID != getUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	if err := feedRepo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.backup != nil {
		h.backup.TriggerAsync()
	}
	c.Status(http.StatusNoContent)
}

func (h *FeedHandler) FetchNow(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	feedRepo := h.repo.WithCtx(c)
	feed, err := feedRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}

	if feed.OwnerID == nil || *feed.OwnerID != getUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	log.Printf("Manual fetch triggered for feed: %s", feed.URL)
	articleRepo := h.articleRepo.WithCtx(c)

	// HTML feeds use scraping instead of RSS parsing
	if feed.FeedType == "html" {
		htmlFeed, err := h.fetcher.FetchHTML(c.Request.Context(), feed.URL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "fetch failed: " + err.Error()})
			return
		}
		if err := feedRepo.UpdateFetchInfo(feed.ID, "", "", time.Now()); err != nil {
			log.Printf("Failed to update feed info: %v", err)
		}
		if htmlFeed.Title != "" {
			bestEffort(c, "feed auto-title", func() error { return feedRepo.UpdateTitle(feed.ID, htmlFeed.Title) })
		}
		newCount := 0
		attempts := 0
		for _, item := range htmlFeed.Items {
			if attempts >= 10 {
				break
			}
			if item.Link == "" {
				continue
			}
			exists, _ := articleRepo.Exists(feed.ID, item.Link)
			if exists {
				continue
			}
			attempts++
			content, _ := h.contentFetcher.FetchContent(c.Request.Context(), item.Link)
			article := &model.Article{
				FeedID:      feed.ID,
				Title:       item.Title,
				URL:         item.Link,
				Content:     content,
				PublishedAt: publishedTime(item.PublishedParsed, item.UpdatedParsed),
			}
			article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
			if err := articleRepo.Create(article); err != nil {
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

	if err := feedRepo.UpdateFetchInfo(feed.ID, result.ETag, result.LastModified, time.Now()); err != nil {
		log.Printf("Failed to update feed info: %v", err)
	}
	if result.Feed != nil && result.Feed.Title != "" {
		if err := feedRepo.UpdateTitle(feed.ID, result.Feed.Title); err != nil {
			log.Printf("Failed to update feed title: %v", err)
		}
	}

	newCount := 0
	for _, item := range result.Feed.Items {
		if newCount >= 10 {
			break
		}

		exists, _ := articleRepo.Exists(feed.ID, item.Link)
		mediaInfo := rss.ExtractVideoMedia(item.Link)
		if mediaInfo == nil {
			mediaInfo = rss.ExtractMedia(item)
		}
		if exists {
			content, enriched := rss.BuildItemContent(item.Description, item.Content, item.Link)
			if enriched {
				wordCount, readingMinutes := rss.ComputeMetrics(content)
				updated := false
				bestEffort(c, "enriched article content", func() error {
					var err error
					updated, err = articleRepo.UpdateEnrichedContentIfChanged(feed.ID, item.Link, content, wordCount, readingMinutes)
					return err
				})
				if updated {
					log.Printf("Refreshed enriched content for: %s", item.Link)
				}
			}
			articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, publishedTime(item.PublishedParsed, item.UpdatedParsed))
			if mediaInfo != nil {
				if err := articleRepo.UpdateMediaIfNull(feed.ID, item.Link, mediaInfo.URL, mediaInfo.Type, mediaInfo.Duration); err != nil {
					log.Printf("Failed to backfill media for %s: %v", item.Link, err)
				}
			}
			continue
		}

		content, _ := rss.BuildItemContent(item.Description, item.Content, item.Link)
		mediaType := ""
		if mediaInfo != nil {
			mediaType = mediaInfo.Type
		}

		if rss.ShouldDeepFetchArticle(feed.FeedType, item.Link, mediaType) && item.Link != "" {
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

		if err := articleRepo.Create(article); err != nil {
			log.Printf("Failed to create article: %v", err)
		} else {
			newCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "fetch completed",
		"new_articles": newCount,
		"feed_title":   result.Feed.Title,
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
	feeds, err := h.repo.WithCtx(c).GetVisibleByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	document := opmlDocument{Version: "2.0"}
	document.Head.Title = "RSS Pal Subscriptions"
	document.Body.Outlines = make([]opmlFeed, 0, len(feeds))
	for _, feed := range feeds {
		parsed, err := url.Parse(feed.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			continue
		}
		switch feed.FeedType {
		case "", "rss", "html", "youtube", "podcast":
		default:
			continue
		}
		title := feed.Title
		if title == "" {
			title = feed.URL
		}
		feedType := "rss"
		if feed.FeedType == "html" {
			feedType = "html"
		}
		document.Body.Outlines = append(document.Body.Outlines, opmlFeed{Type: feedType, Text: title, Title: title, URL: feed.URL})
	}
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法生成 OPML 文件"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=rss-pal-subscriptions.opml")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", append([]byte(xml.Header), body...))
}

type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title string `xml:"title"`
	} `xml:"head"`
	Body struct {
		Outlines []opmlFeed `xml:"outline"`
	} `xml:"body"`
}

type opmlFeed struct {
	Type  string `xml:"type,attr"`
	Text  string `xml:"text,attr"`
	Title string `xml:"title,attr"`
	URL   string `xml:"xmlUrl,attr"`
}

// UpdateStatus PATCH /api/feeds/:id/status
func (h *FeedHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	feed, err := h.repo.WithCtx(c).GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}
	if feed.OwnerID == nil || *feed.OwnerID != getUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.WithCtx(c).UpdateStatus(id, req.Status); err != nil {
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
	feed, err := h.repo.WithCtx(c).GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed not found"})
		return
	}
	if feed.OwnerID == nil || *feed.OwnerID != getUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	var req struct {
		PriorityWeight float64 `json:"priority_weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.WithCtx(c).UpdateWeight(id, req.PriorityWeight); err != nil {
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

// CreateOneoffLinkSet handles POST /api/feeds/oneoff_link_set.
//
// When expand=true: creates a pseudo-feed (feed_type='link_set', is_active=false,
// expand_links=true) owned by the caller; fetches the URL's content synchronously
// and inserts a parent article. The worker pass detects links_extendable within seconds.
//
// When expand=false: saves the URL as a single article into the user's "⭐ 网摘"
// feed (GetOrCreateClipFeed), with no link expansion.
func (h *FeedHandler) CreateOneoffLinkSet(c *gin.Context) {
	h.createOneoffLinkSet(c, true)
}

// CaptureFromShortcut is limited to saving a single private clip. A shortcut
// credential cannot create expanding feeds, even if expand=true is supplied.
func (h *FeedHandler) CaptureFromShortcut(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	var req struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请共享有效的 http 或 https 网页链接"})
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请共享有效的 http 或 https 网页链接"})
		return
	}
	normalized, err := normalizeFeedURL(u.String())
	if err != nil || validatePublicURL(normalized) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请共享有效的公开网页链接"})
		return
	}
	// No external requests: acknowledge only after the private queue row commits.
	article, err := h.articleRepo.WithCtx(c).EnqueueCapture(getUserID(c), normalized)
	if err != nil {
		log.Printf("shortcut enqueue failed for owner=%d: %v", getUserID(c), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法加入抓取队列"})
		return
	}
	status := http.StatusAccepted
	if article.ProcessingState == "ready" {
		status = http.StatusOK
	}
	jsonAfterCommit(c, status, gin.H{"feed_id": article.FeedID, "parent_article_id": article.ID, "processing_state": article.ProcessingState})
}

func (h *FeedHandler) createOneoffLinkSet(c *gin.Context, allowExpand bool) {
	var req struct {
		URL    string `json:"url"`
		Expand bool   `json:"expand"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url required"})
		return
	}
	if !allowExpand {
		parsed, err := url.Parse(req.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请共享有效的 http 或 https 网页链接"})
			return
		}
	}
	req.Expand = req.Expand && allowExpand
	var err error
	req.URL, err = normalizeFeedURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL"})
		return
	}
	if err := validatePublicURL(req.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	canonicalHN, isHN := rss.HackerNewsItemURL(req.URL)
	if isHN {
		if canonicalHN == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 HN 帖子 ID"})
			return
		}
		req.URL, req.Expand = canonicalHN, false
	}
	// HN metadata and discussion use the same reader as re-fetch and capture.
	result, err := h.contentFetcher.FetchContentWithMetadata(ctx, req.URL)
	if errors.Is(err, rss.ErrHackerNewsUnavailable) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": rss.ErrHackerNewsUnavailable.Error()})
		return
	}
	content := result.Content
	if err != nil || content == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法抓取该页面内容"})
		return
	}

	// 2. Derive a title from the page's <title>.
	title := req.URL
	if result.Title != "" {
		title = result.Title
	}
	if !isHN {
		if doc, derr := h.contentFetcher.FetchHTMLDocument(ctx, req.URL); derr == nil && doc != nil {
			if pageTitle := strings.TrimSpace(doc.Find("title").First().Text()); pageTitle != "" {
				title = pageTitle
			}
		}
	}

	ownerID := getOwnerID(c)
	wc, rm := rss.ComputeMetrics(content)
	if isHN && ownerID != nil {
		articles := h.articleRepo.WithCtx(c)
		existing, err := articles.FindByOwnerAndURL(*ownerID, req.URL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
			return
		}
		if existing != nil {
			if err := articles.UpdateContent(existing.ID, content, wc, rm); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新文章失败"})
				return
			}
			if err := articles.UpdateTitle(existing.ID, title); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新标题失败"})
				return
			}
			if err := articles.UpdateSummary(existing.ID, "", ""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新摘要失败"})
				return
			}
			jsonAfterCommit(c, http.StatusOK, gin.H{"feed_id": existing.FeedID, "parent_article_id": existing.ID})
			return
		}
	}

	var feedID int
	var article *model.Article

	if req.Expand {
		// ==== expand=true: existing link_set behaviour ====
		// 3. Create the pseudo-feed.
		feed := &model.Feed{
			URL:              req.URL,
			Title:            title,
			FetchIntervalMin: 60,
			IsActive:         false,
			OwnerID:          ownerID,
			FeedType:         "link_set",
			ExpandLinks:      true,
		}
		feedRepo := h.repo.WithCtx(c)
		if err := feedRepo.Create(feed); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 4. Set feed to paused status (matches is_active=false).
		if err := feedRepo.UpdateStatus(feed.ID, "paused"); err != nil {
			log.Printf("oneoff_link_set: set paused status: %v", err)
		}

		article = &model.Article{
			FeedID:         feed.ID,
			Title:          title,
			URL:            req.URL,
			Content:        content,
			WordCount:      wc,
			ReadingMinutes: rm,
		}
		feedID = feed.ID
	} else {
		// ==== expand=false: save into the user's ⭐ 网摘 feed ====
		if ownerID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "需要登录"})
			return
		}
		saved, err := h.repo.WithCtx(c).GetOrCreateClipFeed(*ownerID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		article = &model.Article{
			FeedID:         saved.ID,
			Title:          title,
			URL:            req.URL,
			Content:        content,
			WordCount:      wc,
			ReadingMinutes: rm,
		}
		feedID = saved.ID
	}

	if isHN {
		article.IsClip = true
	}
	// 5. Insert the article.
	if err := h.articleRepo.WithCtx(c).Create(article); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文章失败: " + err.Error()})
		return
	}

	jsonAfterCommit(c, http.StatusCreated, gin.H{
		"feed_id":           feedID,
		"parent_article_id": article.ID,
	})
}

// normalizeFeedURL preserves the existing no-scheme convenience while treating
// HTTP schemes case-insensitively and emitting a canonical lowercase scheme.
func normalizeFeedURL(rawURL string) (string, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return "", fmt.Errorf("empty URL")
	}
	if !strings.HasPrefix(strings.ToLower(value), "http://") && !strings.HasPrefix(strings.ToLower(value), "https://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	return u.String(), nil
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
	if u.User != nil {
		return fmt.Errorf("URL credentials are not allowed")
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
