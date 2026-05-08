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
	tagRepo  *repository.UserTagRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewUserTagHandler(tagRepo *repository.UserTagRepository, bindRepo *repository.ArticleUserTagRepository) *UserTagHandler {
	return &UserTagHandler{tagRepo: tagRepo, bindRepo: bindRepo}
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
	tags, err := h.tagRepo.GetTagsForUser(getUserID(c))
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
	id, err := h.tagRepo.CreateTag(getUserID(c), name)
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
	err = h.tagRepo.RenameTag(getUserID(c), id, name)
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
	err = h.tagRepo.DeleteTag(getUserID(c), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}
