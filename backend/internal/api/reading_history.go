package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type ReadingHistoryHandler struct {
	repo *repository.ReadingHistoryRepository
}

func NewReadingHistoryHandler(repo *repository.ReadingHistoryRepository) *ReadingHistoryHandler {
	return &ReadingHistoryHandler{repo: repo}
}

func (h *ReadingHistoryHandler) Record(c *gin.Context) {
	kind := c.Param("kind")
	articleID, err := strconv.ParseInt(c.Param("id"), 10, 32)
	if err != nil || articleID <= 0 || (kind != "article" && kind != "explore") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article"})
		return
	}

	err = h.repo.WithCtx(c).Record(getUserID(c), int(articleID), kind == "explore")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
	case err != nil:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record article open"})
	default:
		c.Status(http.StatusNoContent)
	}
}

func (h *ReadingHistoryHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
		return
	}

	var before *time.Time
	var beforeID int64
	if cursor := c.Query("before"); cursor != "" {
		rawTime, rawID, _ := strings.Cut(cursor, ",")
		openedAt, timeErr := time.Parse(time.RFC3339Nano, rawTime)
		beforeID, err = strconv.ParseInt(rawID, 10, 64)
		if timeErr != nil || err != nil || beforeID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid history cursor"})
			return
		}
		before = &openedAt
	}

	items, err := h.repo.WithCtx(c).List(getUserID(c), limit+1, before, beforeID)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load reading history"})
		return
	}
	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = last.OpenedAt.UTC().Format(time.RFC3339Nano) + "," + strconv.FormatInt(last.ID, 10)
		items = items[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}
