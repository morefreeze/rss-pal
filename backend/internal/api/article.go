package api

import (
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
	if err := h.progressRepo.MarkAllRead(getUserID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已全部标记为已读"})
}

func NewArticleHandler(articleRepo *repository.ArticleRepository, progressRepo *repository.ProgressRepository, summarizer *service.SummarizerService) *ArticleHandler {
	return &ArticleHandler{
		articleRepo:  articleRepo,
		progressRepo: progressRepo,
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

	articles, err := h.articleRepo.GetAll(limit, offset, feedID, unreadOnly, getUserID(c))
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

	article, err := h.articleRepo.GetByID(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	// Get reading progress
	progress, _ := h.progressRepo.GetByArticleAndUser(id, getUserID(c))

	response := gin.H{
		"article":  article,
		"progress": progress,
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

	// Check optional template_id — accept from either JSON body or query param
	var brief, detailed string
	if h.templateRepo != nil {
		var bodyReq struct {
			TemplateID int `json:"template_id"`
		}
		// ShouldBindJSON is non-fatal here; fall through if body has no template_id
		_ = c.ShouldBindJSON(&bodyReq)
		templateIDStr := c.Query("template_id")
		if bodyReq.TemplateID == 0 && templateIDStr != "" {
			bodyReq.TemplateID, _ = strconv.Atoi(templateIDStr)
		}
		if bodyReq.TemplateID > 0 {
			templateID := bodyReq.TemplateID
			{
				tpl, err := h.templateRepo.GetByID(templateID)
				if err == nil && tpl != nil {
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

func (h *ArticleHandler) RecordClick(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Click will be handled by preference handler
	c.Status(http.StatusOK)
}
