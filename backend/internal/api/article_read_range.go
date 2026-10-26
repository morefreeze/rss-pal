package api

import (
	"errors"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type readRangeRequest struct {
	AnchorID  int    `json:"anchor_id" binding:"required,gt=0"`
	Direction string `json:"direction" binding:"required,oneof=single above below"`
	Sort      string `json:"sort" binding:"omitempty,oneof=published captured"`
	Order     string `json:"order" binding:"omitempty,oneof=asc desc"`
	FeedID    *int   `json:"feed_id" binding:"omitempty,gt=0"`
	TagID     *int   `json:"tag_id" binding:"omitempty,gt=0"`
	Untagged  bool   `json:"untagged"`
	Unread    bool   `json:"unread"`
	Saved     bool   `json:"saved"`
	Clip      bool   `json:"clip"`
	Source    string `json:"source"`
}

func (h *ArticleHandler) MarkReadRange(c *gin.Context) {
	var req readRangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的已读范围"})
		return
	}
	if req.Source != "" {
		kind, value, ok := strings.Cut(req.Source, ":")
		if !req.Clip || !ok || value == "" || (kind != "feed" && kind != "host") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的来源筛选"})
			return
		}
	}
	sort, dir := repository.SortPublished, repository.SortDesc
	if req.Sort == "captured" {
		sort = repository.SortCaptured
	}
	if req.Order == "asc" {
		dir = repository.SortAsc
	}
	ids, err := h.progressRepo.WithCtx(c).MarkReadRange(repository.ReadRange{
		Filter:   repository.ArticleFilter{UserID: getUserID(c), FeedID: req.FeedID, TagID: req.TagID, Untagged: req.Untagged, UnreadOnly: req.Unread, SavedOnly: req.Saved},
		AnchorID: req.AnchorID, Direction: req.Direction, Sort: sort, Dir: dir, ClipOnly: req.Clip, Source: req.Source,
	})
	if errors.Is(err, repository.ErrReadAnchorNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不在当前列表中，请刷新后重试"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记已读失败，请重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": len(ids), "article_ids": ids})
}
