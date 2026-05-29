package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

// briefingIndexMaxSpanDays caps the from/to range. Keeps the SQL bounded
// and the JSON payload small even when the user requests months of data.
const briefingIndexMaxSpanDays = 400

type BriefingIndexHandler struct {
	dailyRepo  *repository.DailyDigestRepository
	weeklyRepo *repository.WeeklyDigestRepository
}

func NewBriefingIndexHandler(dailyRepo *repository.DailyDigestRepository, weeklyRepo *repository.WeeklyDigestRepository) *BriefingIndexHandler {
	return &BriefingIndexHandler{dailyRepo: dailyRepo, weeklyRepo: weeklyRepo}
}

func parseBriefingIndexType(s string) (string, error) {
	if s == "daily" || s == "weekly" {
		return s, nil
	}
	return "", fmt.Errorf("type 必须是 daily 或 weekly")
}

func parseBriefingIndexRange(from, to string) (time.Time, time.Time, error) {
	if from == "" {
		return time.Time{}, time.Time{}, errors.New("from 必填")
	}
	if to == "" {
		return time.Time{}, time.Time{}, errors.New("to 必填")
	}
	f, err := ParseDailyDate(from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from 格式错误: %w", err)
	}
	t, err := ParseDailyDate(to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to 格式错误: %w", err)
	}
	if f.After(t) {
		return time.Time{}, time.Time{}, errors.New("from 不能晚于 to")
	}
	if t.Sub(f) > time.Duration(briefingIndexMaxSpanDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("范围超过 %d 天上限", briefingIndexMaxSpanDays)
	}
	return f, t, nil
}

// Get serves GET /api/briefing/index?type=&from=&to=
func (h *BriefingIndexHandler) Get(c *gin.Context) {
	userID := getUserID(c)
	kind, err := parseBriefingIndexType(c.Query("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	from, to, err := parseBriefingIndexRange(c.Query("from"), c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()

	if kind == "daily" {
		days, dErr := h.dailyRepo.ListDaysInRange(userID, from, to)
		if dErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": dErr.Error()})
			return
		}
		today := TodayLabel(now)
		c.JSON(http.StatusOK, gin.H{
			"type":                 "daily",
			"today_label":          today.Format("2006-01-02"),
			"pending_window_start": today.AddDate(0, 0, -3).Format("2006-01-02"),
			"cached":               formatDates(days),
		})
		return
	}

	// weekly
	weeks, wErr := h.weeklyRepo.ListWeeksInRange(userID, from, to)
	if wErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": wErr.Error()})
		return
	}
	thisWeek := MondayLabel(now)
	c.JSON(http.StatusOK, gin.H{
		"type":                 "weekly",
		"this_week_start":      thisWeek.Format("2006-01-02"),
		"pending_window_start": thisWeek.AddDate(0, 0, -7).Format("2006-01-02"),
		"cached":               formatDates(weeks),
	})
}

func formatDates(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format("2006-01-02")
	}
	return out
}
