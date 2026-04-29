package api

import (
	"net/http"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type InsightsHandler struct {
	prefRepo     *repository.PreferenceRepository
	templateRepo *repository.TemplateRepository
	summarizer   *ai.Summarizer
	cfg          *config.Config
}

func NewInsightsHandler(prefRepo *repository.PreferenceRepository, templateRepo *repository.TemplateRepository, summarizer *ai.Summarizer, cfg *config.Config) *InsightsHandler {
	return &InsightsHandler{
		prefRepo:     prefRepo,
		templateRepo: templateRepo,
		summarizer:   summarizer,
		cfg:          cfg,
	}
}

func (h *InsightsHandler) Generate(c *gin.Context) {
	userID := getUserID(c)

	topics, err := h.prefRepo.GetTopicStrings(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{"insights": "", "message": "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章"})
		return
	}

	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)

	summarizerToUse := h.summarizer
	if h.templateRepo != nil {
		aiCfg, err := h.templateRepo.GetUserAIConfig(userID)
		if err == nil && aiCfg != nil && aiCfg.APIKey != "" {
			baseURL := aiCfg.BaseURL
			if baseURL == "" {
				baseURL = h.cfg.Claude.BaseURL
			}
			summarizerToUse = ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
		}
	}

	insights, err := summarizerToUse.GenerateInsights(c.Request.Context(), topics, strings.Join(titles, "\n"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成洞察失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"insights": insights})
}
