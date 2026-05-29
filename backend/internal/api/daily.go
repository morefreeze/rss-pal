package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

// briefingShanghai is shared by daily and (eventually) weekly handlers.
// Fixed offset so we don't depend on tzdata in containers.
var briefingShanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// briefingDayCutoffHour: window for "day D" is [D 05:00, D+1 05:00) Asia/Shanghai.
const briefingDayCutoffHour = 5

// briefingMaxLookbackDays: GET refuses requests further back than this.
const briefingMaxLookbackDays = 30

// TodayLabel returns the calendar date D in Asia/Shanghai such that
// `now` falls inside [D 05:00, D+1 05:00). Before 05:00 the label is yesterday.
func TodayLabel(now time.Time) time.Time {
	t := now.In(briefingShanghai)
	if t.Hour() < briefingDayCutoffHour {
		t = t.AddDate(0, 0, -1)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, briefingShanghai)
}

// ParseDailyDate parses YYYY-MM-DD in Asia/Shanghai and returns the date at 00:00.
func ParseDailyDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty date")
	}
	return time.ParseInLocation("2006-01-02", s, briefingShanghai)
}

// DailyWindow returns the [start, end) bounds for the briefing day D.
// start = D 05:00, end = D+1 05:00 (both Asia/Shanghai).
func DailyWindow(day time.Time) (time.Time, time.Time) {
	d := day.In(briefingShanghai)
	start := time.Date(d.Year(), d.Month(), d.Day(), briefingDayCutoffHour, 0, 0, 0, briefingShanghai)
	end := start.AddDate(0, 0, 1)
	return start, end
}

// MondayLabel returns the Monday at 00:00 in Asia/Shanghai of the week containing `now`.
// Symmetric with TodayLabel but week-anchored.
func MondayLabel(now time.Time) time.Time {
	t := now.In(briefingShanghai)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	mon := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, briefingShanghai)
}

type DailyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.DailyDigestRepository
}

func NewDailyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.DailyDigestRepository) *DailyHandler {
	return &DailyHandler{articleRepo: articleRepo, digestRepo: digestRepo}
}

// Get serves GET /api/daily-digest?date=YYYY-MM-DD.
func (h *DailyHandler) Get(c *gin.Context) {
	userID := getUserID(c)
	now := time.Now()
	today := TodayLabel(now)

	// explicitDate is true iff the caller passed ?date=. We only do the
	// "fall back one day if requested is missing" trick for the default
	// case (no param) so that opening /daily fresh in the morning still
	// shows something even if yesterday isn't generated yet. When the
	// user explicitly clicks a date in the calendar we respect their
	// pick and surface the pending state instead of jumping back.
	explicitDate := false
	requested := today.AddDate(0, 0, -1)
	if q := c.Query("date"); q != "" {
		parsed, err := ParseDailyDate(q)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date 必须是 YYYY-MM-DD 格式"})
			return
		}
		requested = parsed
		explicitDate = true
	}

	if requested.After(today) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date 不能晚于今天"})
		return
	}
	lookbackLimit := today.AddDate(0, 0, -briefingMaxLookbackDays)
	if requested.Before(lookbackLimit) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date 超出回溯范围"})
		return
	}

	// Live branch: in-progress today
	if requested.Equal(today) {
		start, _ := DailyWindow(today)
		articles, err := h.articleRepo.GetTopArticlesInRange(userID, start, now, 5)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if articles == nil {
			articles = []model.Article{}
		}
		c.JSON(http.StatusOK, gin.H{
			"requested_date": requested.Format("2006-01-02"),
			"shown_date":     requested.Format("2006-01-02"),
			"pending":        false,
			"intro_text":     "",
			"articles":       articles,
			"mode":           "live",
		})
		return
	}

	// Cached branch: try requested, then fall back one day if missing.
	dd, err := h.digestRepo.Get(userID, requested)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dd != nil {
		h.respondCached(c, userID, requested, requested, false, dd)
		return
	}
	fallback := requested.AddDate(0, 0, -1)
	if !explicitDate && !fallback.Before(lookbackLimit) {
		fb, ferr := h.digestRepo.Get(userID, fallback)
		if ferr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": ferr.Error()})
			return
		}
		if fb != nil {
			h.respondCached(c, userID, requested, fallback, true, fb)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"requested_date": requested.Format("2006-01-02"),
		"shown_date":     requested.Format("2006-01-02"),
		"pending":        true,
		"intro_text":     "",
		"articles":       []model.Article{},
		"mode":           "pending",
	})
}

func (h *DailyHandler) respondCached(c *gin.Context, userID int, requested, shown time.Time, pending bool, dd *repository.DailyDigest) {
	ids := make([]int, len(dd.ArticleIDs))
	for i, id := range dd.ArticleIDs {
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
		"requested_date": requested.Format("2006-01-02"),
		"shown_date":     shown.Format("2006-01-02"),
		"pending":        pending,
		"intro_text":     dd.IntroText,
		"articles":       articles,
		"mode":           "cached",
	})
}
