package api

import (
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type WeeklyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.WeeklyDigestRepository
}

// NewWeeklyHandler constructs a read-only weekly digest handler. The worker
// is the sole writer of weekly_digests; the API never invokes the summarizer.
func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo}
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

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

	// Default to last week (this Monday - 7 days) since the worker generates
	// "last week" on the Monday 05:00 cron tick — the current week's digest
	// doesn't exist yet. Symmetric with daily's "default to yesterday".
	weekStart := startOfWeek(time.Now()).AddDate(0, 0, -7)
	if w := c.Query("week"); w != "" {
		parsed, err := time.ParseInLocation("2006-01-02", w, shanghai)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "week 必须是 YYYY-MM-DD 格式"})
			return
		}
		weekStart = startOfWeek(parsed)
	}

	cached, err := h.digestRepo.Get(userID, weekStart)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if cached == nil {
		c.JSON(http.StatusOK, gin.H{
			"week_start": weekStart.Format("2006-01-02"),
			"intro_text": "",
			"articles":   []model.Article{},
			"pending":    true,
		})
		return
	}

	ids := make([]int, len(cached.ArticleIDs))
	for i, id := range cached.ArticleIDs {
		ids[i] = int(id)
	}
	articles, err := h.articleRepo.GetByIDsForUser(userID, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}
	c.JSON(http.StatusOK, gin.H{
		"week_start": weekStart.Format("2006-01-02"),
		"intro_text": cached.IntroText,
		"articles":   articles,
		"pending":    false,
	})
}
