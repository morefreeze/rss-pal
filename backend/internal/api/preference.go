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
	prefRepo    *repository.PreferenceRepository
	articleRepo *repository.ArticleRepository
}

func NewPreferenceHandler(prefRepo *repository.PreferenceRepository, articleRepo *repository.ArticleRepository) *PreferenceHandler {
	return &PreferenceHandler{prefRepo: prefRepo, articleRepo: articleRepo}
}

// applyCachedClassification, when the article already has a cached classification,
// upserts the topic + tags into the user's interest tables synchronously. Silently
// no-ops when the article is not yet classified (worker will pick it up).
func (h *PreferenceHandler) applyCachedClassification(c *gin.Context, userID, articleID int, signalType string, signalValue float64) {
	if h.articleRepo == nil {
		return
	}
	topic, tags, err := h.articleRepo.WithCtx(c).GetClassification(articleID)
	if err != nil || topic == "" {
		return
	}
	strength := StrengthFromSignal(signalType, signalValue)
	if strength <= 0 {
		return
	}
	tw := SignalToTopicWeight(strength)
	gw := SignalToTagWeight(strength)
	_ = h.prefRepo.UpsertTopic(userID, topic, tw)
	for _, t := range tags {
		_ = h.prefRepo.UpsertTag(userID, t, gw)
	}
}

// dampenCachedClassification mirrors applyCachedClassification but with a negative
// magnitude. Used for `dislike` to nudge the user's interest profile away from the
// article's topic/tags. No-op when the article has no cached classification (worker
// will pick it up later, by which time the article is already hidden via the per-article
// dislike score).
func (h *PreferenceHandler) dampenCachedClassification(c *gin.Context, userID, articleID int) {
	if h.articleRepo == nil {
		return
	}
	topic, tags, err := h.articleRepo.WithCtx(c).GetClassification(articleID)
	if err != nil || topic == "" {
		return
	}
	const dampenStrength = 0.5
	topicDelta := -SignalToTopicWeight(dampenStrength)
	tagDelta := -SignalToTagWeight(dampenStrength)
	_ = h.prefRepo.DampenTopic(userID, topic, topicDelta)
	for _, t := range tags {
		_ = h.prefRepo.DampenTag(userID, t, tagDelta)
	}
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

	h.applyCachedClassification(c, pref.UserID, pref.ArticleID, "like", 1.0)
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

	h.dampenCachedClassification(c, pref.UserID, pref.ArticleID)
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

	h.applyCachedClassification(c, pref.UserID, pref.ArticleID, "save", 1.0)
	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) Unsave(c *gin.Context) {
	var req model.PreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.prefRepo.DeleteSignal(getUserID(c), req.ArticleID, "save"); err != nil {
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

	h.applyCachedClassification(c, pref.UserID, pref.ArticleID, "read_duration", req.DurationSeconds)
	c.Status(http.StatusOK)
}

func (h *PreferenceHandler) GetTopics(c *gin.Context) {
	topics, err := h.prefRepo.GetTopics(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, topics)
}

func (h *PreferenceHandler) GetTags(c *gin.Context) {
	tags, err := h.prefRepo.GetTags(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tags)
}

func (h *PreferenceHandler) DeleteTopic(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.prefRepo.DeleteTopic(getUserID(c), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PreferenceHandler) DeleteTag(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.prefRepo.DeleteTag(getUserID(c), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

type ProgressHandler struct {
	repo      *repository.ProgressRepository
	eventRepo *repository.EventRepository
}

func NewProgressHandler(repo *repository.ProgressRepository, eventRepo *repository.EventRepository) *ProgressHandler {
	return &ProgressHandler{repo: repo, eventRepo: eventRepo}
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

	userID := getUserID(c)
	progress := &model.ReadingProgress{
		UserID:         userID,
		ArticleID:      articleID,
		ScrollPosition: req.ScrollPosition,
		LastReadAt:     time.Now(),
		IsCompleted:    req.IsCompleted,
	}

	result, err := h.repo.Upsert(progress)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if result.NewlyCompleted && h.eventRepo != nil {
		_ = h.eventRepo.Insert(userID, articleID, model.EventTypeCompletedRead)
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
