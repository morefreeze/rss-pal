package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type SavedHandler struct {
	saved    *repository.SavedRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewSavedHandler(saved *repository.SavedRepository, bindRepo *repository.ArticleUserTagRepository) *SavedHandler {
	return &SavedHandler{saved: saved, bindRepo: bindRepo}
}

// GET /api/saved
func (h *SavedHandler) List(c *gin.Context) {
	userID := getUserID(c)

	q := repository.SavedQuery{
		UserID: userID,
		Mode:   strings.ToLower(c.DefaultQuery("mode", "and")),
		Limit:  20,
		Offset: 0,
	}

	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			q.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			q.Offset = n
		}
	}
	if c.Query("untagged") == "true" {
		q.Untagged = true
	} else if v := c.Query("tag_ids"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
				q.TagIDs = append(q.TagIDs, n)
			}
		}
	}
	if v := c.Query("source_feed_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.SourceFeedID = n
		}
	}

	articles, total, err := h.saved.ListSaved(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}

	// Attach manual tags per article
	ids := make([]int, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	tagMap, err := h.bindRepo.GetManualForArticles(ids, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type item struct {
		model.Article
		ManualTags []model.UserTag `json:"manual_tags"`
	}
	out := make([]item, len(articles))
	for i, a := range articles {
		out[i] = item{Article: a, ManualTags: tagMap[a.ID]}
		if out[i].ManualTags == nil {
			out[i].ManualTags = []model.UserTag{}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"total": total,
	})
}
