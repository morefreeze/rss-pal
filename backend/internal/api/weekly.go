package api

import (
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type WeeklyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.WeeklyDigestRepository
	summarizer  *ai.Summarizer
}

func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository, summarizer *ai.Summarizer) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo, summarizer: summarizer}
}

// shanghai is fixed (UTC+8, no DST). Hardcoded so we don't depend on the
// container's tzdata being present.
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// startOfWeek returns the Monday 00:00 in Asia/Shanghai for the calendar week
// containing `t`.
func startOfWeek(t time.Time) time.Time {
	t = t.In(shanghai)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, shanghai)
}

func (h *WeeklyHandler) Get(c *gin.Context) {
	userID := getUserID(c)

	weekStart := startOfWeek(time.Now())
	if w := c.Query("week"); w != "" {
		parsed, err := time.ParseInLocation("2006-01-02", w, shanghai)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "week 必须是 YYYY-MM-DD 格式"})
			return
		}
		weekStart = startOfWeek(parsed)
	}
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Cache lookup first — if present, honor the frozen article snapshot.
	cached, _ := h.digestRepo.Get(userID, weekStart)

	var (
		articles []model.Article
		intro    string
	)
	if cached != nil {
		ids := make([]int, len(cached.ArticleIDs))
		for i, id := range cached.ArticleIDs {
			ids[i] = int(id)
		}
		var err error
		articles, err = h.articleRepo.GetByIDsForUser(userID, ids)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		intro = cached.IntroText
	} else {
		var err error
		articles, err = h.articleRepo.GetTopArticlesInRange(userID, weekStart, weekEnd, 10)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if cached == nil && len(articles) > 0 && h.summarizer != nil {
		items := make([]ai.WeeklyDigestItem, 0, len(articles))
		for _, a := range articles {
			items = append(items, ai.WeeklyDigestItem{Title: a.Title, SummaryBrief: a.SummaryBrief})
		}
		generated, gerr := h.summarizer.GenerateWeeklyIntro(c.Request.Context(), items)
		if gerr == nil && generated != "" {
			intro = generated
			ids := make([]int, 0, len(articles))
			for _, a := range articles {
				ids = append(ids, a.ID)
			}
			if uerr := h.digestRepo.Upsert(userID, weekStart, intro, ids); uerr != nil {
				c.Writer.Header().Set("X-Digest-Cache-Error", uerr.Error())
			}
		} else if gerr != nil {
			c.Writer.Header().Set("X-Digest-AI-Error", gerr.Error())
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"week_start": weekStart.Format("2006-01-02"),
		"intro_text": intro,
		"articles":   articles,
	})
}
