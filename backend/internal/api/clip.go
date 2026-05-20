package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type ClipHandler struct {
	clip     *repository.ClipRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewClipHandler(clip *repository.ClipRepository, bindRepo *repository.ArticleUserTagRepository) *ClipHandler {
	return &ClipHandler{clip: clip, bindRepo: bindRepo}
}

// GET /api/clip
func (h *ClipHandler) List(c *gin.Context) {
	userID := getUserID(c)

	q := repository.ClipQuery{
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
	if v := c.Query("source"); v != "" {
		if i := strings.Index(v, ":"); i > 0 {
			kind := v[:i]
			value := v[i+1:]
			if kind == "feed" || kind == "host" {
				q.SourceKind = kind
				q.SourceValue = value
			}
		}
	}

	if c.Query("sort") == "captured" {
		q.Sort = repository.SortCaptured
	}
	if c.Query("order") == "asc" {
		q.Dir = repository.SortAsc
	}
	if c.Query("unread") == "true" {
		q.UnreadOnly = true
	}
	if c.Query("saved") == "true" {
		q.SavedOnly = true
	}

	rows, total, err := h.clip.ListClipped(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []repository.ClipRow{}
	}

	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.Article.ID
	}
	tagMap, err := h.bindRepo.GetManualForArticles(ids, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type item struct {
		model.Article
		ManualTags      []model.UserTag            `json:"manual_tags"`
		EffectiveSource repository.EffectiveSource `json:"effective_source"`
	}
	out := make([]item, len(rows))
	for i, r := range rows {
		out[i] = item{
			Article:         r.Article,
			ManualTags:      tagMap[r.Article.ID],
			EffectiveSource: r.EffectiveSource,
		}
		if out[i].ManualTags == nil {
			out[i].ManualTags = []model.UserTag{}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"total": total,
	})
}
