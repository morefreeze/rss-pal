package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type InsightsHandler struct {
	prefRepo         *repository.PreferenceRepository
	articleRepo      *repository.ArticleRepository
	templateRepo     *repository.TemplateRepository
	userInsightsRepo *repository.UserInsightRepository
	summarizer       *ai.Summarizer
	cfg              *config.Config
}

func NewInsightsHandler(prefRepo *repository.PreferenceRepository, articleRepo *repository.ArticleRepository,
	templateRepo *repository.TemplateRepository, userInsightsRepo *repository.UserInsightRepository,
	summarizer *ai.Summarizer, cfg *config.Config) *InsightsHandler {
	return &InsightsHandler{
		prefRepo:         prefRepo,
		articleRepo:      articleRepo,
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

// Latest returns the most recent insight + quota + per-recommendation article
// metadata so the frontend can render clickable cards without an extra round-trip.
func (h *InsightsHandler) Latest(c *gin.Context) {
	userID := getUserID(c)
	ins, _ := h.userInsightsRepo.GetLatest(userID)
	quota, _ := h.computeQuota(userID)
	resp := gin.H{
		"insight":         ins,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	}
	if ins != nil && len(ins.Recommendations) > 0 {
		ids := make([]int, 0)
		seen := map[int]bool{}
		for _, d := range ins.Recommendations {
			for _, a := range d.Articles {
				if !seen[a.ArticleID] {
					seen[a.ArticleID] = true
					ids = append(ids, a.ArticleID)
				}
			}
		}
		if len(ids) > 0 {
			arts, err := h.articleRepo.GetByIDsForUser(userID, ids)
			if err != nil {
				log.Printf("insights: Latest GetByIDsForUser user=%d: %v", userID, err)
			} else {
				meta := make(map[string]gin.H, len(arts))
				for _, a := range arts {
					brief := []rune(a.SummaryBrief)
					if len(brief) > 80 {
						brief = brief[:80]
					}
					meta[strconv.Itoa(a.ID)] = gin.H{
						"id":         a.ID,
						"title":      a.Title,
						"feed_title": a.FeedTitle,
						"brief":      string(brief),
						"is_read":    a.IsRead,
					}
				}
				resp["rec_articles"] = meta
			}
		}
	}
	c.JSON(http.StatusOK, resp)
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

	topics, err := h.prefRepo.GetTopics(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":          "no_data",
			"message":         "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}
	tags, _ := h.prefRepo.GetTags(userID)
	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)
	candidates, err := h.articleRepo.GetInsightCandidates(userID, 40, 10)
	if err != nil {
		log.Printf("insights: GetInsightCandidates user=%d: %v", userID, err)
		candidates = nil
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

	prompt := ai.BuildInsightPrompt(topics, tags, titles, candidates)

	go h.runAsyncManual(id, userID, summarizer, prompt, candidates)

	c.JSON(http.StatusAccepted, gin.H{
		"status":          "pending",
		"id":              id,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InsightsHandler) runAsyncManual(id, userID int, s *ai.Summarizer, prompt string, candidates []model.InsightCandidate) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncGenTimeout)
	defer cancel()
	raw, err := s.GenerateUserInsightJSON(ctx, prompt)
	if err != nil {
		log.Printf("insights: async user=%d id=%d failed: %v", userID, id, err)
		_ = h.userInsightsRepo.MarkFailed(id, err.Error())
		return
	}
	idSet := make(map[int]bool, len(candidates))
	for _, c := range candidates {
		idSet[c.Article.ID] = true
	}
	markdown, recs, dropped := ai.ParseInsightJSON(raw, idSet)
	if len(dropped) > 0 {
		log.Printf("insights: user=%d id=%d dropped %d entries: %v", userID, id, len(dropped), dropped)
	}
	if err := h.userInsightsRepo.MarkDoneWithRecs(id, markdown, recs); err != nil {
		log.Printf("insights: async user=%d id=%d MarkDoneWithRecs: %v", userID, id, err)
		return
	}
	log.Printf("insights: async user=%d id=%d ok (%dB md, %d recs)", userID, id, len(markdown), len(recs))
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
