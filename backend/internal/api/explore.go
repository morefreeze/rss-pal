package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type exploreStore interface {
	GetPage(userID int, params repository.ExploreListParams) (*repository.ExplorePage, error)
	GetSources(userID int) ([]repository.ExploreSourceItem, error)
	GetVisibleArticle(userID, articleID int) (*repository.ExploreArticleDetail, error)
	CreateFeedback(userID int, input repository.ExploreFeedbackInput) (*model.ExploreFeedback, error)
	DeleteFeedback(userID, feedbackID int) error
	ReplaceInterests(userID int, topics []string) ([]model.ExploreFeedback, error)
	RecordArticleEvent(userID, articleID int, eventType string, occurredAt time.Time) (bool, error)
}

type ExploreHandler struct {
	storeFor func(*gin.Context) exploreStore
	now      func() time.Time
}

func NewExploreHandler(repo *repository.ExploreRepository) *ExploreHandler {
	return &ExploreHandler{
		storeFor: func(c *gin.Context) exploreStore { return repo.WithCtx(c) },
		now:      time.Now,
	}
}

func newExploreHandlerWithStore(store exploreStore, now func() time.Time) *ExploreHandler {
	return &ExploreHandler{storeFor: func(*gin.Context) exploreStore { return store }, now: now}
}

func (h *ExploreHandler) GetExplore(c *gin.Context) {
	params, ok := parseExploreListParams(c)
	if !ok {
		return
	}
	page, err := h.storeFor(c).GetPage(getUserID(c), params)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	if page == nil {
		page = &repository.ExplorePage{Articles: []repository.ExploreArticleListItem{}}
	}
	if page.Articles == nil {
		page.Articles = []repository.ExploreArticleListItem{}
	}
	next := explorelogic.ExploreScheduleAt(h.now()).NextSlotAt
	page.Snapshot.NextRefreshAt = &next
	c.Header("Cache-Control", "private, no-cache")
	c.JSON(http.StatusOK, page)
}

func parseExploreListParams(c *gin.Context) (repository.ExploreListParams, bool) {
	params := repository.ExploreListParams{Limit: 20, Sort: repository.SortPublished, Dir: repository.SortDesc}
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be an integer"})
			return params, false
		}
		if value < 1 {
			value = 1
		}
		if value > repository.MaxExplorePageSize {
			value = repository.MaxExplorePageSize
		}
		params.Limit = value
	}
	if raw := c.Query("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be an integer"})
			return params, false
		}
		if value < 0 {
			value = 0
		}
		params.Offset = value
	}
	if raw := c.Query("sort"); raw != "" {
		switch raw {
		case "published":
			params.Sort = repository.SortPublished
		case "captured":
			params.Sort = repository.SortCaptured
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "sort must be published or captured"})
			return params, false
		}
	}
	order, dir := c.Query("order"), c.Query("dir")
	if order != "" && dir != "" && order != dir {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order and dir must agree"})
		return params, false
	}
	if order == "" {
		order = dir
	}
	if order != "" {
		switch order {
		case "desc":
			params.Dir = repository.SortDesc
		case "asc":
			params.Dir = repository.SortAsc
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "order must be asc or desc"})
			return params, false
		}
	}
	params.Topic = strings.TrimSpace(c.Query("topic"))
	if len([]rune(params.Topic)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "topic is too long"})
		return params, false
	}
	return params, true
}

func (h *ExploreHandler) GetSources(c *gin.Context) {
	items, err := h.storeFor(c).GetSources(getUserID(c))
	if err != nil {
		writeExploreError(c, err)
		return
	}
	if items == nil {
		items = []repository.ExploreSourceItem{}
	}
	c.Header("Cache-Control", "private, no-cache")
	c.JSON(http.StatusOK, items)
}

func (h *ExploreHandler) GetArticle(c *gin.Context) {
	id, ok := positiveExploreID(c, "id")
	if !ok {
		return
	}
	article, err := h.storeFor(c).GetVisibleArticle(getUserID(c), id)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-cache")
	c.JSON(http.StatusOK, article)
}

func (h *ExploreHandler) CreateFeedback(c *gin.Context) {
	var request struct {
		FeedbackType string  `json:"feedback_type"`
		SourceID     *int    `json:"source_id"`
		Topic        *string `json:"topic"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid feedback body"})
		return
	}
	input := repository.ExploreFeedbackInput{
		FeedbackType: request.FeedbackType,
		SourceID:     request.SourceID,
		Topic:        request.Topic,
	}
	if input.Topic != nil {
		topic := strings.TrimSpace(*input.Topic)
		input.Topic = &topic
	}
	if !validExploreFeedbackRequest(input) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid explore feedback"})
		return
	}
	feedback, err := h.storeFor(c).CreateFeedback(getUserID(c), input)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.JSON(http.StatusOK, feedback)
}

func validExploreFeedbackRequest(input repository.ExploreFeedbackInput) bool {
	switch input.FeedbackType {
	case model.ExploreFeedbackHideSource:
		return input.SourceID != nil && *input.SourceID > 0 && input.Topic == nil
	case model.ExploreFeedbackDampenTopic, model.ExploreFeedbackBoostTopic:
		return input.SourceID == nil && input.Topic != nil && repository.IsExploreInterest(*input.Topic)
	default:
		return false
	}
}

func (h *ExploreHandler) DeleteFeedback(c *gin.Context) {
	id, ok := positiveExploreID(c, "id")
	if !ok {
		return
	}
	if err := h.storeFor(c).DeleteFeedback(getUserID(c), id); err != nil {
		writeExploreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ExploreHandler) ReplaceInterests(c *gin.Context) {
	var request struct {
		Topics []string `json:"topics"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Topics == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "topics must be an array"})
		return
	}
	for _, topic := range request.Topics {
		if !repository.IsExploreInterest(topic) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported interest topic"})
			return
		}
	}
	feedback, err := h.storeFor(c).ReplaceInterests(getUserID(c), request.Topics)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	if feedback == nil {
		feedback = []model.ExploreFeedback{}
	}
	c.JSON(http.StatusOK, gin.H{"interests": feedback})
}

func (h *ExploreHandler) RecordArticleEvent(c *gin.Context) {
	id, ok := positiveExploreID(c, "id")
	if !ok {
		return
	}
	var request struct {
		EventType string `json:"event_type"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !validExploreEventType(request.EventType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid explore event"})
		return
	}
	recorded, err := h.storeFor(c).RecordArticleEvent(getUserID(c), id, request.EventType, h.now())
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recorded": recorded})
}

func validExploreEventType(value string) bool {
	return value == model.ExploreArticleEventExposure ||
		value == model.ExploreArticleEventClick ||
		value == model.ExploreArticleEventCompletedRead
}

func positiveExploreID(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func writeExploreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrExploreNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "explore resource not found"})
	case errors.Is(err, repository.ErrInvalidExploreFeedback),
		errors.Is(err, repository.ErrInvalidExploreEvent),
		errors.Is(err, repository.ErrInvalidExploreInterest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
