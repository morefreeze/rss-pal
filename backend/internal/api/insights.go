package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type InsightsHandler struct {
	prefRepo         *repository.PreferenceRepository
	templateRepo     *repository.TemplateRepository
	userInsightsRepo *repository.UserInsightRepository
	summarizer       *ai.Summarizer
	cfg              *config.Config
}

func NewInsightsHandler(prefRepo *repository.PreferenceRepository, templateRepo *repository.TemplateRepository,
	userInsightsRepo *repository.UserInsightRepository, summarizer *ai.Summarizer, cfg *config.Config) *InsightsHandler {
	return &InsightsHandler{
		prefRepo:         prefRepo,
		templateRepo:     templateRepo,
		userInsightsRepo: userInsightsRepo,
		summarizer:       summarizer,
		cfg:              cfg,
	}
}

const (
	dailyManualLimit   = 3
	monthlyManualLimit = 100
)

type insightQuota struct {
	RemainingToday int `json:"remaining_today"`
	RemainingMonth int `json:"remaining_month"`
}

func (h *InsightsHandler) computeQuota(userID int) (insightQuota, bool) {
	today, _ := h.userInsightsRepo.CountManualSince(userID, "1 day")
	month, _ := h.userInsightsRepo.CountManualSince(userID, "30 days")
	q := insightQuota{
		RemainingToday: dailyManualLimit - today,
		RemainingMonth: monthlyManualLimit - month,
	}
	if q.RemainingToday < 0 {
		q.RemainingToday = 0
	}
	if q.RemainingMonth < 0 {
		q.RemainingMonth = 0
	}
	return q, q.RemainingToday > 0 && q.RemainingMonth > 0
}

// Latest returns the most recent insight + quota.
func (h *InsightsHandler) Latest(c *gin.Context) {
	userID := getUserID(c)
	ins, _ := h.userInsightsRepo.GetLatest(userID)
	quota, _ := h.computeQuota(userID)
	c.JSON(http.StatusOK, gin.H{
		"insight":         ins,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InsightsHandler) chooseSummarizer(userID int) *ai.Summarizer {
	if h.templateRepo == nil {
		return h.summarizer
	}
	aiCfg, err := h.templateRepo.GetUserAIConfig(userID)
	if err != nil || aiCfg == nil || aiCfg.APIKey == "" {
		return h.summarizer
	}
	baseURL := aiCfg.BaseURL
	if baseURL == "" {
		baseURL = h.cfg.Claude.BaseURL
	}
	return ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
}

// Generate (non-streaming, kept for backward compat). Streaming variant in Task 13.
func (h *InsightsHandler) Generate(c *gin.Context) {
	if c.Query("stream") == "1" {
		h.GenerateStream(c)
		return
	}

	userID := getUserID(c)
	quota, ok := h.computeQuota(userID)
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":           "quota_exceeded",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	topics, err := h.prefRepo.GetTopicStrings(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"insights":        "",
			"message":         "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)
	summarizer := h.chooseSummarizer(userID)

	insights, err := summarizer.GenerateInsights(c.Request.Context(), topics, strings.Join(titles, "\n"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成洞察失败: " + err.Error()})
		return
	}

	if err := h.userInsightsRepo.Insert(userID, insights, "manual", summarizer.Model()); err != nil {
		// Persistence failure is non-fatal: still return content; just log via header.
		c.Header("X-Insight-Save-Error", err.Error())
	}

	quota, _ = h.computeQuota(userID)
	c.JSON(http.StatusOK, gin.H{
		"insights":        insights,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

// GenerateStream is implemented in insights_stream.go (Task 13). For now this
// stub returns 501 so Generate can call it without the package failing to build.
func (h *InsightsHandler) GenerateStream(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "streaming not implemented yet"})
}

// parseIDParam parses :id from the route. Returns (id, true) on success;
// writes 400 + returns (0, false) on failure.
func parseIDParam(c *gin.Context, key string) (int, bool) {
	v := c.Param(key)
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}
	return id, true
}
