package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	repo *repository.EventRepository
}

func NewEventHandler(repo *repository.EventRepository) *EventHandler {
	return &EventHandler{repo: repo}
}

// Create logs a behavioral event for the authenticated user.
// POST /api/events  body: { article_id: int, event_type: "exposure" | "click" }
// Note: completed_read events are written by the backend in ProgressHandler;
// this endpoint only accepts exposure and click from the frontend.
func (h *EventHandler) Create(c *gin.Context) {
	var req struct {
		ArticleID int    `json:"article_id"`
		EventType string `json:"event_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ArticleID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "article_id required"})
		return
	}
	if req.EventType != "exposure" && req.EventType != "click" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event_type must be exposure or click"})
		return
	}
	userID := getUserID(c)
	if err := h.repo.WithCtx(c).Insert(userID, req.ArticleID, req.EventType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
