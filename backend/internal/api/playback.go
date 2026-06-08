package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type PlaybackHandler struct {
	repo     *repository.PlaybackProgressRepository
	prefRepo *repository.PreferenceRepository
}

func NewPlaybackHandler(repo *repository.PlaybackProgressRepository, prefRepo *repository.PreferenceRepository) *PlaybackHandler {
	return &PlaybackHandler{repo: repo, prefRepo: prefRepo}
}

// Get returns the user's saved position for an article. Missing row → zero values.
// Response: { "position_seconds": int, "is_completed": bool }
func (h *PlaybackHandler) Get(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	userID := getUserID(c)
	p, err := h.repo.WithCtx(c).Get(userID, articleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusOK, gin.H{"position_seconds": 0, "is_completed": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"position_seconds": p.PositionSeconds, "is_completed": p.IsCompleted})
}

// Put upserts the user's position. On the first transition false→true, also
// writes a completed_listen user_preferences row so the recommender treats
// "listened all the way through" as a strong positive signal. The weight is
// applied in the scoring SQL (Task 7 of the podcast plan); the row itself
// stores SignalValue=1.0 as a count.
//
// Body: { "position_seconds": int, "is_completed": bool }
func (h *PlaybackHandler) Put(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	var req struct {
		PositionSeconds int  `json:"position_seconds"`
		IsCompleted     bool `json:"is_completed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PositionSeconds < 0 {
		req.PositionSeconds = 0
	}
	userID := getUserID(c)

	result, err := h.repo.WithCtx(c).Upsert(userID, articleID, req.PositionSeconds, req.IsCompleted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if result.NewlyCompleted {
		err := h.prefRepo.WithCtx(c).Add(&model.UserPreference{
			UserID:      userID,
			ArticleID:   articleID,
			SignalType:  "completed_listen",
			SignalValue: 1.0, // weight is applied in the scoring SQL; this row stores the count.
		})
		if err != nil {
			// Unique violation = a concurrent PUT already wrote this signal. Expected and benign.
			if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
				// ignore
			} else {
				log.Printf("playback: completed_listen signal failed user=%d article=%d: %v", userID, articleID, err)
			}
		}
	}
	c.Status(http.StatusOK)
}
