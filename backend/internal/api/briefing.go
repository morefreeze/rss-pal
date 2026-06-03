package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type BriefingHandler struct {
	userRepo *repository.UserRepository
}

func NewBriefingHandler(userRepo *repository.UserRepository) *BriefingHandler {
	return &BriefingHandler{userRepo: userRepo}
}

// ValidateBriefingTab returns true iff `tab` is a known enum value.
func ValidateBriefingTab(tab string) bool {
	return tab == "daily" || tab == "weekly"
}

// GetLastTab serves GET /api/briefing/last-tab.
func (h *BriefingHandler) GetLastTab(c *gin.Context) {
	userID := getUserID(c)
	tab, err := h.userRepo.WithCtx(c).GetBriefingLastTab(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ValidateBriefingTab(tab) {
		tab = "daily"
	}
	c.JSON(http.StatusOK, gin.H{"tab": tab})
}

// SetLastTab serves POST /api/briefing/last-tab.
func (h *BriefingHandler) SetLastTab(c *gin.Context) {
	var body struct {
		Tab string `json:"tab"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ValidateBriefingTab(body.Tab) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tab 必须是 daily 或 weekly"})
		return
	}
	userID := getUserID(c)
	if err := h.userRepo.WithCtx(c).SetBriefingLastTab(userID, body.Tab); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tab": body.Tab})
}
