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
	ClearNegativeFeedback(userID int) (int, error)
	ReplaceInterests(userID int, topics []string) ([]model.ExploreFeedback, error)
	RecordArticleEvent(userID, articleID int, eventType string, occurredAt time.Time) (bool, error)
}

type exploreSubscriber interface {
	SubscribeOne(userID, sourceID int) (explorelogic.SubscribeResult, error)
	Subscribe(userID int, sourceIDs []int) ([]explorelogic.SubscribeResult, error)
}

type ExploreHandler struct {
	storeFor      func(*gin.Context) exploreStore
	subscriberFor func(*gin.Context) exploreSubscriber
	now           func() time.Time
}

func NewExploreHandler(repo *repository.ExploreRepository) *ExploreHandler {
	return &ExploreHandler{
		storeFor: func(c *gin.Context) exploreStore { return repo.WithCtx(c) },
		now:      time.Now,
	}
}

func NewExploreHandlerWithSubscriber(repo *repository.ExploreRepository, subscriber *explorelogic.SubscribeService) *ExploreHandler {
	handler := NewExploreHandler(repo)
	handler.subscriberFor = func(c *gin.Context) exploreSubscriber { return subscriber.WithCtx(c) }
	return handler
}

func newExploreHandlerWithStore(store exploreStore, now func() time.Time) *ExploreHandler {
	return &ExploreHandler{storeFor: func(*gin.Context) exploreStore { return store }, now: now}
}

func newExploreHandlerWithStores(store exploreStore, subscriber exploreSubscriber, now func() time.Time) *ExploreHandler {
	return &ExploreHandler{
		storeFor:      func(*gin.Context) exploreStore { return store },
		subscriberFor: func(*gin.Context) exploreSubscriber { return subscriber },
		now:           now,
	}
}

func (h *ExploreHandler) GetExplore(c *gin.Context) {
	params, ok := parseExploreListParams(c)
	if !ok {
		return
	}
	userID := getUserID(c)
	page, err := h.storeFor(c).GetPage(userID, params)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	if page == nil {
		page = &repository.ExplorePage{Articles: []repository.ExploreArticleListItem{}, Interests: []string{}}
	}
	if page.Articles == nil {
		page.Articles = []repository.ExploreArticleListItem{}
	}
	if page.Interests == nil {
		page.Interests = []string{}
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
	userID := getUserID(c)
	items, err := h.storeFor(c).GetSources(userID)
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
	case model.ExploreFeedbackDampenTopic:
		return input.SourceID == nil && input.Topic != nil && *input.Topic != "" && len([]rune(*input.Topic)) <= 100
	case model.ExploreFeedbackBoostTopic:
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

func (h *ExploreHandler) ClearNegativeFeedback(c *gin.Context) {
	deleted, err := h.storeFor(c).ClearNegativeFeedback(getUserID(c))
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted_count": deleted})
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

func (h *ExploreHandler) SubscribeSource(c *gin.Context) {
	sourceID, ok := positiveExploreID(c, "id")
	if !ok {
		return
	}
	if h.subscriberFor == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	result, err := h.subscriberFor(c).SubscribeOne(getUserID(c), sourceID)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.JSON(http.StatusOK, struct {
		FeedID         int  `json:"feed_id"`
		Created        bool `json:"created"`
		CopiedArticles int  `json:"copied_articles"`
	}{FeedID: result.FeedID, Created: result.Created, CopiedArticles: result.CopiedArticles})
}

func (h *ExploreHandler) SubscribeSources(c *gin.Context) {
	var request struct {
		SourceIDs []int `json:"source_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !validExploreSubscribeIDs(request.SourceIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_ids must contain unique positive ids"})
		return
	}
	if h.subscriberFor == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	results, err := h.subscriberFor(c).Subscribe(getUserID(c), request.SourceIDs)
	if err != nil {
		writeExploreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func validExploreSubscribeIDs(sourceIDs []int) bool {
	if len(sourceIDs) == 0 || len(sourceIDs) > explorelogic.MaxSubscribeSources {
		return false
	}
	seen := make(map[int]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID <= 0 {
			return false
		}
		if _, duplicate := seen[sourceID]; duplicate {
			return false
		}
		seen[sourceID] = struct{}{}
	}
	return true
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
	case errors.Is(err, explorelogic.ErrSubscribeSourceUnavailable):
		c.JSON(http.StatusNotFound, gin.H{"error": "explore source not found"})
	case errors.Is(err, repository.ErrInvalidExploreFeedback),
		errors.Is(err, repository.ErrInvalidExploreEvent),
		errors.Is(err, repository.ErrInvalidExploreInterest),
		errors.Is(err, explorelogic.ErrInvalidSubscribeRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
