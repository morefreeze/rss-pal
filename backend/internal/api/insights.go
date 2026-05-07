package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	asyncGenTimeout    = 5 * time.Minute
)

type insightQuota struct {
	RemainingToday int `json:"remaining_today"`
	RemainingMonth int `json:"remaining_month"`
}

func (h *InsightsHandler) computeQuota(userID int) (insightQuota, bool) {
	today, _ := h.userInsightsRepo.CountManualSince(userID, 24*time.Hour)
	month, _ := h.userInsightsRepo.CountManualSince(userID, 30*24*time.Hour)
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

// Generate kicks off an async insight job. Returns immediately with the
// updated quota; the actual AI call runs in a background goroutine and
// updates the user_insights row from 'pending' to 'done' (or 'failed').
func (h *InsightsHandler) Generate(c *gin.Context) {
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
			"status":          "no_data",
			"message":         "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	summarizer := h.chooseSummarizer(userID)
	id, err := h.userInsightsRepo.InsertPending(userID, "manual", summarizer.Model())
	if err != nil {
		if errors.Is(err, repository.ErrPendingExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error":           "already_pending",
				"remaining_today": quota.RemainingToday,
				"remaining_month": quota.RemainingMonth,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)
	prompt := buildSimplePrompt(topics, titles)

	go h.runAsyncManual(id, userID, summarizer, prompt)

	c.JSON(http.StatusAccepted, gin.H{
		"status":          "pending",
		"id":              id,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InsightsHandler) runAsyncManual(id, userID int, s *ai.Summarizer, prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncGenTimeout)
	defer cancel()
	content, err := s.GenerateUserInsight(ctx, prompt)
	if err != nil {
		log.Printf("insights: async user=%d id=%d failed: %v", userID, id, err)
		_ = h.userInsightsRepo.MarkFailed(id, err.Error())
		return
	}
	if err := h.userInsightsRepo.MarkDone(id, content); err != nil {
		log.Printf("insights: async user=%d id=%d MarkDone: %v", userID, id, err)
		return
	}
	log.Printf("insights: async user=%d id=%d ok (%dB)", userID, id, len(content))
}

func buildSimplePrompt(topics, titles []string) string {
	return "基于用户的兴趣主题和最近阅读，请用中文 markdown 给出洞察分析（核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）：\n\n" +
		"## 用户兴趣主题（按权重排序）\n" + strings.Join(topics, "\n") + "\n\n" +
		"## 最近阅读的文章标题\n" + strings.Join(titles, "\n")
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
