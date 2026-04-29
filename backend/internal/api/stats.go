package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type StatsHandler struct {
	statsRepo *repository.StatsRepository
}

func NewStatsHandler(statsRepo *repository.StatsRepository) *StatsHandler {
	return &StatsHandler{statsRepo: statsRepo}
}

func (h *StatsHandler) GetStats(c *gin.Context) {
	userID := getUserID(c)
	stats, err := h.statsRepo.GetStats(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *StatsHandler) GetProgress(c *gin.Context) {
	userID := getUserID(c)
	progress, err := h.statsRepo.GetFetchProgress(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, progress)
}
