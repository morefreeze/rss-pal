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
	now         func() time.Time
}

// NewWeeklyHandler constructs a read-only weekly digest handler. The worker
// is the sole writer of weekly_digests; the API never invokes the summarizer.
func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo, now: time.Now}
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
	now := h.now()

	// Default to last week (this Monday - 7 days) since the worker generates
	// "last week" on the Monday 05:00 cron tick — the current week's digest
	// doesn't exist yet. Symmetric with daily's "default to yesterday".
	weekStart := startOfWeek(now).AddDate(0, 0, -7)
	if w := c.Query("week"); w != "" {
		parsed, err := time.ParseInLocation("2006-01-02", w, shanghai)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "week 必须是 YYYY-MM-DD 格式"})
			return
		}
		weekStart = startOfWeek(parsed)
	}

	cached, err := h.digestRepo.WithCtx(c).Get(userID, weekStart)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if cached == nil {
		metadata := WeeklyGenerationMetadataAt(now, weekStart, false)
		response := gin.H{
			"week_start":        weekStart.Format("2006-01-02"),
			"intro_text":        "",
			"articles":          []model.Article{},
			"pending":           metadata.Pending,
			"generation_status": metadata.Status,
		}
		if metadata.EstimatedGenerationAt != nil {
			response["estimated_generation_at"] = metadata.EstimatedGenerationAt.Format(time.RFC3339)
		}
		c.JSON(http.StatusOK, response)
		return
	}

	ids := make([]int, len(cached.ArticleIDs))
	for i, id := range cached.ArticleIDs {
		ids[i] = int(id)
	}
	articles, err := h.articleRepo.WithCtx(c).GetByIDsForUser(userID, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}
	c.JSON(http.StatusOK, gin.H{
		"week_start":        weekStart.Format("2006-01-02"),
		"intro_text":        cached.IntroText,
		"articles":          articles,
		"pending":           false,
		"generation_status": WeeklyGenerationReady,
	})
}
