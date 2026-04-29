package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type PreferenceHandler struct {
	prefRepo *repository.PreferenceRepository
}

func NewPreferenceHandler(prefRepo *repository.PreferenceRepository) *PreferenceHandler {
	return &PreferenceHandler{prefRepo: prefRepo}
}

func (h *PreferenceHandler) Like(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pref := &model.UserPreference{
		UserID:      getUserID(c),
		ArticleID:   req.ArticleID,
		SignalType:  "like",
		SignalValue: 1.0,
	}

	if err := h.prefRepo.Add(pref); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) Dislike(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pref := &model.UserPreference{
		UserID:      getUserID(c),
		ArticleID:   req.ArticleID,
		SignalType:  "dislike",
		SignalValue: 1.0,
	}

	if err := h.prefRepo.Add(pref); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) Save(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pref := &model.UserPreference{
		UserID:      getUserID(c),
		ArticleID:   req.ArticleID,
		SignalType:  "save",
		SignalValue: 1.0,
	}

	if err := h.prefRepo.Add(pref); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) RecordReadDuration(c *gin.Context) {
	var req struct {
		ArticleID       int     `json:"article_id"`
		DurationSeconds float64 `json:"duration_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pref := &model.UserPreference{
		UserID:       getUserID(c),
		ArticleID:    req.ArticleID,
		SignalType:   "read_duration",
		SignalValue:  req.DurationSeconds,
	}

	if err := h.prefRepo.Add(pref); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) GetTopics(c *gin.Context) {
	topics, err := h.prefRepo.GetTopics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, topics)
}

type ProgressHandler struct {
	repo *repository.ProgressRepository
}

func NewProgressHandler(repo *repository.ProgressRepository) *ProgressHandler {
	return &ProgressHandler{repo: repo}
}

func (h *ProgressHandler) Get(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("article_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article_id"})
		return
	}

	progress, err := h.repo.GetByArticleAndUser(articleID, getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if progress == nil {
		c.JSON(http.StatusOK, gin.H{"progress": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"progress": progress})
}

func (h *ProgressHandler) Update(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("article_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article_id"})
		return
	}

	var req model.UpdateProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	progress := &model.ReadingProgress{
		UserID:        getUserID(c),
		ArticleID:     articleID,
		ScrollPosition: req.ScrollPosition,
		LastReadAt:    time.Now(),
		IsCompleted:   req.IsCompleted,
	}

	if err := h.repo.Upsert(progress); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, progress)
}

func (h *ProgressHandler) Reset(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("article_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article_id"})
		return
	}

	if err := h.repo.Reset(articleID, getUserID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}
