package api

import (
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
)

type FeedHealthHandler struct {
	repo     *repository.FeedHealthRepository
	feedRepo *repository.FeedRepository
	cfg      config.FeedHealthConfig
}

func NewFeedHealthHandler(repo *repository.FeedHealthRepository, feedRepo *repository.FeedRepository) *FeedHealthHandler {
	return &FeedHealthHandler{repo: repo, feedRepo: feedRepo, cfg: config.DefaultFeedHealth()}
}

// FeedHealthRow is the JSON-serialized per-feed metric for the dashboard.
type FeedHealthRow struct {
	FeedID          int                  `json:"feed_id"`
	FeedTitle       string               `json:"feed_title"`
	Status          string               `json:"status"`
	PriorityWeight  float64              `json:"priority_weight"`
	Produced        int                  `json:"produced"`
	Exposures       int                  `json:"exposures"`
	Clicks          int                  `json:"clicks"`
	CompletedReads  int                  `json:"completed_reads"`
	CTR             *float64             `json:"ctr"`             // null if exposures==0
	ReadCompletion  *float64             `json:"read_completion"` // null if clicks==0
	AvgDurationMin  float64              `json:"avg_duration_min"`
	FeedbackDensity float64              `json:"feedback_density"`
	LastActiveAt    *time.Time           `json:"last_active_at"`
	LastFetchedAt   *time.Time           `json:"last_fetched_at"`
	ValueScore      *float64             `json:"value_score"` // null on cold start
	PruningRule     *service.PruningRule `json:"pruning_rule,omitempty"`
}

type FeedHealthResponse struct {
	Window string          `json:"window"`
	KPI    FeedHealthKPI   `json:"kpi"`
	Rows   []FeedHealthRow `json:"rows"`
}

type FeedHealthKPI struct {
	TotalActive     int `json:"total_active"`
	Healthy         int `json:"healthy"`
	Dormant         int `json:"dormant"`
	CompletedReadsW int `json:"completed_reads_w"`
}

// Get GET /api/feeds/health?window=30d|90d
func (h *FeedHealthHandler) Get(c *gin.Context) {
	windowParam := c.DefaultQuery("window", "30d")
	var window time.Duration
	switch windowParam {
	case "30d":
		window = 30 * 24 * time.Hour
	case "90d":
		window = 90 * 24 * time.Hour
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "window must be 30d or 90d"})
		return
	}

	userID := getUserID(c)
	metrics, err := h.repo.ComputeMetrics(userID, window)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Need feed status/weight — fetch the user-visible feeds and join in app code.
	feeds, err := h.feedRepo.GetVisibleByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	feedMeta := make(map[int]struct {
		Status string
		Weight float64
	}, len(feeds))
	for _, f := range feeds {
		feedMeta[f.ID] = struct {
			Status string
			Weight float64
		}{f.Status, f.PriorityWeight}
	}

	rows := make([]FeedHealthRow, 0, len(metrics))
	totalReads := 0
	healthy, dormant := 0, 0

	for _, m := range metrics {
		// compute value score (NaN if cold start)
		score := service.ComputeValueScore(m)
		var valueScorePtr *float64
		if !isNaN(score) {
			s := score
			valueScorePtr = &s
		}
		m.ValueScore = valueScorePtr

		var ctr, completion *float64
		if m.Exposures > 0 {
			v := float64(m.Clicks) / float64(m.Exposures)
			ctr = &v
		}
		if m.Clicks > 0 {
			v := float64(m.CompletedReads) / float64(m.Clicks)
			completion = &v
		}

		rule := service.EvaluatePruningRule(m, h.cfg)

		meta := feedMeta[m.FeedID]
		row := FeedHealthRow{
			FeedID:          m.FeedID,
			FeedTitle:       m.FeedTitle,
			Status:          meta.Status,
			PriorityWeight:  meta.Weight,
			Produced:        m.Produced,
			Exposures:       m.Exposures,
			Clicks:          m.Clicks,
			CompletedReads:  m.CompletedReads,
			CTR:             ctr,
			ReadCompletion:  completion,
			AvgDurationMin:  m.AvgDurationMin,
			FeedbackDensity: m.FeedbackDensity,
			LastActiveAt:    m.LastActiveAt,
			LastFetchedAt:   m.LastFetchedAt,
			ValueScore:      valueScorePtr,
			PruningRule:     rule,
		}
		rows = append(rows, row)

		totalReads += m.CompletedReads
		if rule == nil && valueScorePtr != nil && *valueScorePtr >= 0.3 {
			healthy++
		}
		if rule != nil && rule.ID == "R2" {
			dormant++
		}
	}

	totalActive := 0
	for _, f := range feeds {
		if f.Status == "active" {
			totalActive++
		}
	}

	c.JSON(http.StatusOK, FeedHealthResponse{
		Window: windowParam,
		KPI: FeedHealthKPI{
			TotalActive:     totalActive,
			Healthy:         healthy,
			Dormant:         dormant,
			CompletedReadsW: totalReads,
		},
		Rows: rows,
	})
}

func isNaN(f float64) bool {
	return f != f
}
