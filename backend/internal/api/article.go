package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
)

type ArticleHandler struct {
	articleRepo  *repository.ArticleRepository
	progressRepo *repository.ProgressRepository
	prefRepo     *repository.PreferenceRepository
	summarizer   *service.SummarizerService
	templateRepo *repository.TemplateRepository
	cfg          *config.Config
}

func (h *ArticleHandler) GetUnreadCount(c *gin.Context) {
	count, err := h.articleRepo.GetUnreadCount(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

func (h *ArticleHandler) MarkAllRead(c *gin.Context) {
	var feedID *int
	if fid := c.Query("feed_id"); fid != "" {
		if id, err := strconv.Atoi(fid); err == nil {
			feedID = &id
		}
	}
	unreadOnly := c.Query("unread") == "true"
	savedOnly := c.Query("saved") == "true"

	if err := h.progressRepo.MarkAllRead(getUserID(c), feedID, unreadOnly, savedOnly); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已全部标记为已读"})
}

func NewArticleHandler(articleRepo *repository.ArticleRepository, progressRepo *repository.ProgressRepository, prefRepo *repository.PreferenceRepository, summarizer *service.SummarizerService) *ArticleHandler {
	return &ArticleHandler{
		articleRepo:  articleRepo,
		progressRepo: progressRepo,
		prefRepo:     prefRepo,
		summarizer:   summarizer,
	}
}

// SetTemplateRepo allows injecting templateRepo after construction (called from main).
func (h *ArticleHandler) SetTemplateRepo(templateRepo *repository.TemplateRepository, cfg *config.Config) {
	h.templateRepo = templateRepo
	h.cfg = cfg
}

func (h *ArticleHandler) GetAll(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	var feedID *int
	if fid := c.Query("feed_id"); fid != "" {
		id, err := strconv.Atoi(fid)
		if err == nil {
			feedID = &id
		}
	}

	unreadOnly := c.Query("unread") == "true"
	savedOnly := c.Query("saved") == "true"

	articles, err := h.articleRepo.GetAll(limit, offset, feedID, unreadOnly, savedOnly, getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) Search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit > 50 {
		limit = 50
	}
	articles, err := h.articleRepo.Search(query, getUserID(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) GetByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	article, feedType, err := h.articleRepo.GetByIDWithFeedType(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	userID := getUserID(c)
	progress, _ := h.progressRepo.GetByArticleAndUser(id, userID)
	signals, _ := h.prefRepo.GetUserSignals(userID, id)

	response := gin.H{
		"article":          article,
		"progress":         progress,
		"signals":          signals,
		"from_bookmarklet": feedType == "saved",
	}
	c.JSON(http.StatusOK, response)
}

func (h *ArticleHandler) GetRecommended(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	articles, err := h.articleRepo.GetRecommended(limit, getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func (h *ArticleHandler) GenerateSummary(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	userID := getUserID(c)

	article, err := h.articleRepo.GetByID(id, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	// Determine which summarizer to use (user-custom or global)
	summarizerToUse := h.summarizer

	if h.templateRepo != nil && h.cfg != nil {
		aiCfg, err := h.templateRepo.GetUserAIConfig(userID)
		if err == nil && aiCfg != nil && aiCfg.APIKey != "" {
			// Build a temporary summarizer from the user's own key/url/model
			baseURL := aiCfg.BaseURL
			if baseURL == "" {
				baseURL = h.cfg.Claude.BaseURL
			}
			userSummarizer := ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
			summarizerToUse = service.NewSummarizerService(userSummarizer)
		}
	}

	// Parse optional template_id from JSON body or query
	var bodyReq struct {
		TemplateID int `json:"template_id"`
	}
	if h.templateRepo != nil {
		_ = c.ShouldBindJSON(&bodyReq)
		if templateIDStr := c.Query("template_id"); bodyReq.TemplateID == 0 && templateIDStr != "" {
			bodyReq.TemplateID, _ = strconv.Atoi(templateIDStr)
		}
	}

	if c.Query("stream") == "1" {
		h.streamSummary(c, id, article, summarizerToUse, bodyReq.TemplateID)
		return
	}

	var brief, detailed string

	if h.templateRepo != nil && bodyReq.TemplateID > 0 {
		tpl, terr := h.templateRepo.GetByID(bodyReq.TemplateID)
		if terr == nil && tpl != nil {
			brief, detailed, err = summarizerToUse.SummarizeWithTemplate(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"summary_brief":    brief,
				"summary_detailed": detailed,
			})
			return
		}
	}

	// Default summarization
	brief, detailed, err = summarizerToUse.Summarize(c.Request.Context(), article)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary_brief":    brief,
		"summary_detailed": detailed,
	})
}

func (h *ArticleHandler) streamSummary(c *gin.Context, id int, article *model.Article, summarizerToUse *service.SummarizerService, templateID int) {
	c.Writer.Header().Set("Content-Type", "application/x-ndjson")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeFrame(c, map[string]any{"type": "error", "msg": "streaming unsupported"})
		return
	}

	writeAndFlush := func(frame map[string]any) {
		writeFrame(c, frame)
		flusher.Flush()
	}

	briefDone := false
	onBrief := func(delta string) {
		writeAndFlush(map[string]any{"type": "brief_delta", "text": delta})
	}
	onDetailed := func(delta string) {
		// First detailed delta marks brief phase complete on the wire so the
		// client can switch its rendering pane even before we know the full text.
		if !briefDone {
			briefDone = true
			writeAndFlush(map[string]any{"type": "brief_phase_done"})
		}
		writeAndFlush(map[string]any{"type": "detailed_delta", "text": delta})
	}

	var brief, detailed string
	var serr error
	if h.templateRepo != nil && templateID > 0 {
		tpl, terr := h.templateRepo.GetByID(templateID)
		if terr == nil && tpl != nil {
			brief, detailed, serr = summarizerToUse.SummarizeWithTemplateStream(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt, onBrief, onDetailed,
			)
		} else {
			brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
		}
	} else {
		brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
	}

	if serr != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": serr.Error()})
		return
	}

	writeAndFlush(map[string]any{"type": "brief_done", "text": brief})
	writeAndFlush(map[string]any{"type": "detailed_done", "text": detailed})

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": err.Error()})
		return
	}

	writeAndFlush(map[string]any{"type": "done"})
}

func writeFrame(c *gin.Context, frame map[string]any) {
	bs, err := json.Marshal(frame)
	if err != nil {
		return
	}
	c.Writer.Write(bs)
	c.Writer.Write([]byte("\n"))
}

func (h *ArticleHandler) RecordClick(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Click will be handled by preference handler
	c.Status(http.StatusOK)
}
