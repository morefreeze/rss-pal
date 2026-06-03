package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

const maxTagNameChars = 64

type UserTagHandler struct {
	tagRepo     *repository.UserTagRepository
	bindRepo    *repository.ArticleUserTagRepository
	suggestRepo *repository.TagSuggestionRepository
}

func NewUserTagHandler(
	tagRepo *repository.UserTagRepository,
	bindRepo *repository.ArticleUserTagRepository,
	suggestRepo *repository.TagSuggestionRepository,
) *UserTagHandler {
	return &UserTagHandler{tagRepo: tagRepo, bindRepo: bindRepo, suggestRepo: suggestRepo}
}

func validateTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxTagNameChars {
		return "", errors.New("name too long (max 64 characters)")
	}
	return name, nil
}

// GET /api/tags
func (h *UserTagHandler) ListTags(c *gin.Context) {
	tags, err := h.tagRepo.WithCtx(c).GetTagsForUser(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tags == nil {
		tags = []model.UserTag{}
	}
	c.JSON(http.StatusOK, tags)
}

// POST /api/tags
func (h *UserTagHandler) CreateTag(c *gin.Context) {
	var req model.CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := h.tagRepo.WithCtx(c).CreateTag(getUserID(c), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "name": name})
}

// PATCH /api/tags/:id
func (h *UserTagHandler) RenameTag(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.RenameTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err = h.tagRepo.WithCtx(c).RenameTag(getUserID(c), id, name)
	switch {
	case errors.Is(err, repository.ErrTagNameConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "tag name already exists"})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}

// DELETE /api/tags/:id
func (h *UserTagHandler) DeleteTag(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	err = h.tagRepo.WithCtx(c).DeleteTag(getUserID(c), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}

// GET /api/articles/:id/tags
func (h *UserTagHandler) GetArticleTags(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	userID := getUserID(c)

	bindRepo := h.bindRepo.WithCtx(c)
	source, err := bindRepo.GetSourceForArticle(articleID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	manual, err := bindRepo.GetManualForArticle(articleID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if manual == nil {
		manual = []model.UserTag{}
	}
	suggestions, err := h.suggestRepo.WithCtx(c).SuggestionsForArticle(articleID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if suggestions == nil {
		suggestions = []string{}
	}
	resp := model.ArticleTagsResponse{
		Source:      source,
		Manual:      manual,
		Suggestions: suggestions,
	}
	c.JSON(http.StatusOK, resp)
}

// POST /api/articles/:id/suggestions/dismiss
func (h *UserTagHandler) DismissSuggestion(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.DismissSuggestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.suggestRepo.WithCtx(c).DismissSuggestion(articleID, getUserID(c), name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// POST /api/articles/:id/tags
func (h *UserTagHandler) AddArticleTag(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.AddArticleTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tagID, err := h.bindRepo.WithCtx(c).BindByName(articleID, getUserID(c), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": tagID, "name": name})
}

// DELETE /api/articles/:id/tags/:tagId
func (h *UserTagHandler) RemoveArticleTag(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tagID, err := strconv.Atoi(c.Param("tagId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tagId"})
		return
	}
	err = h.bindRepo.WithCtx(c).Unbind(articleID, tagID, getUserID(c))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "binding not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}

// GetTagSidebar returns the user's tags + counts under the current
// article-list filter. Mirrors the query params of GET /api/articles
// minus the tag scoping itself.
func (h *UserTagHandler) GetTagSidebar(c *gin.Context) {
	userID := getUserID(c)

	var feedID *int
	if s := c.Query("feed_id"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "feed_id must be an integer"})
			return
		}
		feedID = &n
	}
	filter := repository.ArticleFilter{
		UserID:     userID,
		FeedID:     feedID,
		UnreadOnly: c.Query("unread") == "true",
		SavedOnly:  c.Query("saved") == "true",
	}
	data, err := h.tagRepo.WithCtx(c).GetTagsForSidebar(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}
