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

type InterestsHandler struct {
	prefRepo          *repository.PreferenceRepository
	articleRepo       *repository.ArticleRepository
	templateRepo      *repository.TemplateRepository
	userInterestsRepo *repository.UserInterestRepository
	summarizer        *ai.Summarizer
	cfg               *config.Config
}

func NewInterestsHandler(prefRepo *repository.PreferenceRepository, articleRepo *repository.ArticleRepository,
	templateRepo *repository.TemplateRepository, userInterestsRepo *repository.UserInterestRepository,
	summarizer *ai.Summarizer, cfg *config.Config) *InterestsHandler {
	return &InterestsHandler{
		prefRepo:          prefRepo,
		articleRepo:       articleRepo,
		templateRepo:      templateRepo,
		userInterestsRepo: userInterestsRepo,
		summarizer:        summarizer,
		cfg:               cfg,
	}
}

const (
	dailyManualLimit   = 3
	monthlyManualLimit = 100
	asyncGenTimeout    = 5 * time.Minute
)

type interestQuota struct {
	RemainingToday int `json:"remaining_today"`
	RemainingMonth int `json:"remaining_month"`
}

func (h *InterestsHandler) computeQuota(c *gin.Context, userID int) (interestQuota, bool) {
	interestsRepo := h.userInterestsRepo.WithCtx(c)
	today, _ := interestsRepo.CountManualSince(userID, 24*time.Hour)
	month, _ := interestsRepo.CountManualSince(userID, 30*24*time.Hour)
	q := interestQuota{
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

// Latest returns the most recent interest analysis + quota + per-recommendation article
// metadata so the frontend can render clickable cards without an extra round-trip.
func (h *InterestsHandler) Latest(c *gin.Context) {
	h.latest(c, "interest")
}

func (h *InterestsHandler) LatestLegacy(c *gin.Context) {
	h.latest(c, "insight")
}

func newInterestLatestResponse(payloadKey string, interest *model.UserInterest, quota interestQuota) gin.H {
	return gin.H{
		payloadKey:        interest,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	}
}

func (h *InterestsHandler) latest(c *gin.Context, payloadKey string) {
	userID := getUserID(c)
	interest, _ := h.userInterestsRepo.WithCtx(c).GetLatest(userID)
	quota, _ := h.computeQuota(c, userID)
	resp := newInterestLatestResponse(payloadKey, interest, quota)
	if interest != nil && len(interest.Recommendations) > 0 {
		ids := make([]int, 0)
		seen := map[int]bool{}
		for _, d := range interest.Recommendations {
			for _, a := range d.Articles {
				if !seen[a.ArticleID] {
					seen[a.ArticleID] = true
					ids = append(ids, a.ArticleID)
				}
			}
		}
		if len(ids) > 0 {
			arts, err := h.articleRepo.WithCtx(c).GetByIDsForUser(userID, ids)
			if err != nil {
				log.Printf("interests: Latest GetByIDsForUser user=%d: %v", userID, err)
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

func (h *InterestsHandler) chooseSummarizer(c *gin.Context, userID int) *ai.Summarizer {
	if h.templateRepo == nil {
		return h.summarizer
	}
	aiCfg, err := h.templateRepo.WithCtx(c).GetUserAIConfig(userID)
	if err != nil || aiCfg == nil || aiCfg.APIKey == "" {
		return h.summarizer
	}
	baseURL := aiCfg.BaseURL
	if baseURL == "" {
		baseURL = h.cfg.Claude.BaseURL
	}
	model := aiCfg.Model
	if model == "" && h.cfg != nil {
		model = h.cfg.Claude.Model
	}
	return ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, model)
}

// Generate kicks off an async interest job. Returns immediately with the
// updated quota; the actual AI call runs in a background goroutine and
// updates the persisted row from 'pending' to 'done' (or 'failed').
func (h *InterestsHandler) Generate(c *gin.Context) {
	userID := getUserID(c)

	quota, ok := h.computeQuota(c, userID)
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":           "quota_exceeded",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	prefRepo := h.prefRepo.WithCtx(c)
	topics, err := prefRepo.GetTopics(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":          "no_data",
			"message":         "暂无足够的阅读数据来生成兴趣分析，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}
	tags, _ := prefRepo.GetTags(userID)
	titles, _ := prefRepo.GetRecentReadTitles(userID, 20)
	candidates, err := h.articleRepo.WithCtx(c).GetInterestCandidates(userID, 40, 10)
	if err != nil {
		log.Printf("interests: GetInterestCandidates user=%d: %v", userID, err)
		candidates = nil
	}

	summarizer := h.chooseSummarizer(c, userID)
	id, err := h.userInterestsRepo.WithCtx(c).InsertPending(userID, "manual", summarizer.Model())
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

	prompt := ai.BuildInterestPrompt(topics, tags, titles, candidates)

	go h.runAsyncManual(id, userID, summarizer, prompt, candidates)

	c.JSON(http.StatusAccepted, gin.H{
		"status":          "pending",
		"id":              id,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InterestsHandler) runAsyncManual(id, userID int, s *ai.Summarizer, prompt string, candidates []model.InterestCandidate) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncGenTimeout)
	defer cancel()
	raw, err := s.GenerateUserInterestJSON(ctx, prompt)
	if err != nil {
		log.Printf("interests: async user=%d id=%d failed: %v", userID, id, err)
		_ = h.userInterestsRepo.MarkFailed(id, err.Error())
		return
	}
	idSet := make(map[int]bool, len(candidates))
	for _, c := range candidates {
		idSet[c.Article.ID] = true
	}
	markdown, recs, dropped := ai.ParseInterestJSON(raw, idSet)
	if len(dropped) > 0 {
		log.Printf("interests: user=%d id=%d dropped %d entries: %v", userID, id, len(dropped), dropped)
	}
	if err := h.userInterestsRepo.MarkDoneWithRecs(id, markdown, recs); err != nil {
		log.Printf("interests: async user=%d id=%d MarkDoneWithRecs: %v", userID, id, err)
		return
	}
	log.Printf("interests: async user=%d id=%d ok (%dB md, %d recs)", userID, id, len(markdown), len(recs))
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
