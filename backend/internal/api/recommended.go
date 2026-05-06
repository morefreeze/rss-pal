package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type RecommendedHandler struct {
	repo     *repository.RecommendedFeedRepository
	feedRepo *repository.FeedRepository
}

func NewRecommendedHandler(repo *repository.RecommendedFeedRepository, feedRepo *repository.FeedRepository) *RecommendedHandler {
	return &RecommendedHandler{repo: repo, feedRepo: feedRepo}
}

func (h *RecommendedHandler) List(c *gin.Context) {
	userID := getUserID(c)
	items, err := h.repo.ListWithSubscriptionStatus(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

// Subscribe inserts the recommended feed into `feeds` with the current user as
// owner. If the URL already exists (someone else's, or admin's shared seed),
// returns 200 idempotent so the UI can stay simple.
func (h *RecommendedHandler) Subscribe(c *gin.Context) {
	userID := getUserID(c)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	rf, err := h.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "推荐源不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rf.IsBroken {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该来源当前不可用"})
		return
	}

	uid := userID
	feed := &model.Feed{
		URL:              rf.URL,
		Title:            rf.Title,
		FetchIntervalMin: 60,
		OwnerID:          &uid,
		FeedType:         rf.FeedType,
	}
	if err := h.feedRepo.Create(feed); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			// UNIQUE violation — already subscribed. Idempotent success.
			c.JSON(http.StatusOK, gin.H{"status": "already_subscribed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "订阅失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "subscribed", "feed_id": feed.ID})
}
