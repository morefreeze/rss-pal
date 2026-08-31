package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type fakeExploreStore struct {
	page          *repository.ExplorePage
	sources       []repository.ExploreSourceItem
	detail        *repository.ExploreArticleDetail
	feedback      *model.ExploreFeedback
	lastParams    repository.ExploreListParams
	lastUserID    int
	lastArticleID int
	lastFeedback  repository.ExploreFeedbackInput
	lastTopics    []string
	lastEvent     string
	deleteID      int
	clearDeleted  int
	eventCreated  bool
	err           error
}

type fakeExploreSubscriber struct {
	results       []explorelogic.SubscribeResult
	lastUserID    int
	lastSourceIDs []int
	err           error
}

func (f *fakeExploreSubscriber) SubscribeOne(userID, sourceID int) (explorelogic.SubscribeResult, error) {
	f.lastUserID, f.lastSourceIDs = userID, []int{sourceID}
	if len(f.results) == 0 {
		return explorelogic.SubscribeResult{}, f.err
	}
	return f.results[0], f.err
}

func (f *fakeExploreSubscriber) Subscribe(userID int, sourceIDs []int) ([]explorelogic.SubscribeResult, error) {
	f.lastUserID, f.lastSourceIDs = userID, append([]int(nil), sourceIDs...)
	return f.results, f.err
}

func (f *fakeExploreStore) GetPage(userID int, params repository.ExploreListParams) (*repository.ExplorePage, error) {
	f.lastUserID, f.lastParams = userID, params
	return f.page, f.err
}
func (f *fakeExploreStore) GetSources(userID int) ([]repository.ExploreSourceItem, error) {
	f.lastUserID = userID
	return f.sources, f.err
}
func (f *fakeExploreStore) GetVisibleArticle(userID, articleID int) (*repository.ExploreArticleDetail, error) {
	f.lastUserID, f.lastArticleID = userID, articleID
	return f.detail, f.err
}
func (f *fakeExploreStore) CreateFeedback(userID int, input repository.ExploreFeedbackInput) (*model.ExploreFeedback, error) {
	f.lastUserID, f.lastFeedback = userID, input
	return f.feedback, f.err
}
func (f *fakeExploreStore) DeleteFeedback(userID, feedbackID int) error {
	f.lastUserID, f.deleteID = userID, feedbackID
	return f.err
}
func (f *fakeExploreStore) ClearNegativeFeedback(userID int) (int, error) {
	f.lastUserID = userID
	return f.clearDeleted, f.err
}
func (f *fakeExploreStore) ReplaceInterests(userID int, topics []string) ([]model.ExploreFeedback, error) {
	f.lastUserID, f.lastTopics = userID, topics
	return []model.ExploreFeedback{}, f.err
}
func (f *fakeExploreStore) RecordArticleEvent(userID, articleID int, eventType string, _ time.Time) (bool, error) {
	f.lastUserID, f.lastArticleID, f.lastEvent = userID, articleID, eventType
	return f.eventCreated, f.err
}

func TestExploreHandlerListValidatesParametersClampsAndOmitsContent(t *testing.T) {
	now := time.Date(2026, 8, 31, 3, 15, 0, 0, time.UTC)
	store := &fakeExploreStore{page: &repository.ExplorePage{
		Snapshot: repository.ExploreSnapshotStatus{ID: 9},
		Articles: []repository.ExploreArticleListItem{{ID: 3, SourceID: 4, Title: "lean", Excerpt: "brief"}},
	}}
	handler := newExploreHandlerWithStore(store, func() time.Time { return now })
	router := exploreTestRouter(handler)

	w := performExploreRequest(router, http.MethodGet, "/api/explore?limit=500&offset=-8&sort=captured&order=asc&topic=programming", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if store.lastUserID != 42 || store.lastParams.Limit != repository.MaxExplorePageSize || store.lastParams.Offset != 0 || store.lastParams.Sort != repository.SortCaptured || store.lastParams.Dir != repository.SortAsc || store.lastParams.Topic != "programming" {
		t.Fatalf("params=%+v user=%d", store.lastParams, store.lastUserID)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	encoded := w.Body.String()
	if bytes.Contains([]byte(encoded), []byte(`"content"`)) {
		t.Fatalf("list leaked content field: %s", encoded)
	}
	snapshot := payload["snapshot"].(map[string]any)
	if snapshot["next_refresh_at"] != "2026-08-31T14:00:00+08:00" {
		t.Fatalf("next_refresh_at=%v", snapshot["next_refresh_at"])
	}
}

func TestExploreHandlerListRejectsUnknownSortAndOrder(t *testing.T) {
	store := &fakeExploreStore{page: &repository.ExplorePage{}}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))
	for _, path := range []string{"/api/explore?sort=score", "/api/explore?order=sideways"} {
		w := performExploreRequest(router, http.MethodGet, path, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d want 400; body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestExploreHandlerSourcesExposeUnsubscribableCatalogState(t *testing.T) {
	mergedInto := 9
	store := &fakeExploreStore{sources: []repository.ExploreSourceItem{{
		ID: 7, Title: "retired", ValidationStatus: model.ExploreValidationInvalid,
		IsBroken: true, MergedIntoSourceID: &mergedInto,
	}}}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))

	w := performExploreRequest(router, http.MethodGet, "/api/explore/sources", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload[0]["is_broken"] != true || payload[0]["merged_into_source_id"] != float64(mergedInto) {
		t.Fatalf("source state missing: %s", w.Body.String())
	}
}

func TestExploreHandlerListAcceptsDirAliasAndRejectsConflictingDirection(t *testing.T) {
	store := &fakeExploreStore{page: &repository.ExplorePage{}}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))
	w := performExploreRequest(router, http.MethodGet, "/api/explore?dir=asc", nil)
	if w.Code != http.StatusOK || store.lastParams.Dir != repository.SortAsc {
		t.Fatalf("dir alias status=%d params=%+v body=%s", w.Code, store.lastParams, w.Body.String())
	}
	w = performExploreRequest(router, http.MethodGet, "/api/explore?order=asc&dir=desc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("conflicting direction status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExploreHandlerFeedbackInterestsUndoAndEvents(t *testing.T) {
	feedbackID, sourceID := 17, 7
	store := &fakeExploreStore{
		feedback:     &model.ExploreFeedback{ID: feedbackID, UserID: 42, SourceID: &sourceID, FeedbackType: model.ExploreFeedbackHideSource},
		eventCreated: true,
	}
	router := exploreTestRouter(newExploreHandlerWithStore(store, func() time.Time {
		return time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC)
	}))

	w := performExploreRequest(router, http.MethodPost, "/api/explore/feedback", map[string]any{"feedback_type": "hide_source", "source_id": sourceID})
	if w.Code != http.StatusOK || store.lastFeedback.SourceID == nil || *store.lastFeedback.SourceID != sourceID {
		t.Fatalf("feedback status=%d input=%+v body=%s", w.Code, store.lastFeedback, w.Body.String())
	}
	w = performExploreRequest(router, http.MethodPost, "/api/explore/feedback", map[string]any{"feedback_type": "dampen_topic", "topic": "distributed-systems"})
	if w.Code != http.StatusOK || store.lastFeedback.Topic == nil || *store.lastFeedback.Topic != "distributed-systems" {
		t.Fatalf("dampen status=%d input=%+v body=%s", w.Code, store.lastFeedback, w.Body.String())
	}
	w = performExploreRequest(router, http.MethodDelete, "/api/explore/feedback/17", nil)
	if w.Code != http.StatusNoContent || store.deleteID != feedbackID {
		t.Fatalf("delete status=%d id=%d", w.Code, store.deleteID)
	}
	w = performExploreRequest(router, http.MethodPut, "/api/explore/interests", map[string]any{"topics": []string{"programming", "security"}})
	if w.Code != http.StatusOK || len(store.lastTopics) != 2 {
		t.Fatalf("interests status=%d topics=%v body=%s", w.Code, store.lastTopics, w.Body.String())
	}
	w = performExploreRequest(router, http.MethodPost, "/api/explore/articles/23/events", map[string]any{"event_type": "exposure"})
	if w.Code != http.StatusOK || store.lastArticleID != 23 || store.lastEvent != model.ExploreArticleEventExposure {
		t.Fatalf("event status=%d article=%d event=%q body=%s", w.Code, store.lastArticleID, store.lastEvent, w.Body.String())
	}
}

func TestExploreHandlerClearsOnlyNegativeFeedbackForAuthenticatedUser(t *testing.T) {
	store := &fakeExploreStore{clearDeleted: 3}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))
	w := performExploreRequest(router, http.MethodDelete, "/api/explore/feedback", nil)
	if w.Code != http.StatusOK || store.lastUserID != 42 || w.Body.String() != "{\"deleted_count\":3}" {
		t.Fatalf("clear status=%d user=%d body=%s", w.Code, store.lastUserID, w.Body.String())
	}
}

func TestExploreHandlerRejectsInvalidFeedbackInterestAndEventEnums(t *testing.T) {
	store := &fakeExploreStore{}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))
	tests := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/explore/feedback", map[string]any{"feedback_type": "hide_source"}},
		{http.MethodPost, "/api/explore/feedback", map[string]any{"feedback_type": "unknown", "topic": "programming"}},
		{http.MethodPost, "/api/explore/feedback", map[string]any{"feedback_type": "boost_topic", "topic": "arbitrary prompt"}},
		{http.MethodPut, "/api/explore/interests", map[string]any{"topics": []string{"arbitrary prompt"}}},
		{http.MethodPost, "/api/explore/articles/1/events", map[string]any{"event_type": "share"}},
	}
	for _, tt := range tests {
		w := performExploreRequest(router, tt.method, tt.path, tt.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status=%d want 400 body=%s", tt.method, tt.path, w.Code, w.Body.String())
		}
	}
}

func TestExploreHandlerMapsVisibilityErrorsToNotFound(t *testing.T) {
	store := &fakeExploreStore{err: repository.ErrExploreNotFound}
	router := exploreTestRouter(newExploreHandlerWithStore(store, time.Now))
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/explore/articles/4"},
		{http.MethodDelete, "/api/explore/feedback/4"},
	} {
		w := performExploreRequest(router, tc.method, tc.path, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
	}
	store.err = errors.New("database unavailable")
	w := performExploreRequest(router, http.MethodGet, "/api/explore/sources", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("internal status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExploreHandlerSubscribeSingleAndBatchDTOs(t *testing.T) {
	subscriber := &fakeExploreSubscriber{results: []explorelogic.SubscribeResult{
		{SourceID: 7, FeedID: 70, Created: true, CopiedArticles: 3},
		{SourceID: 8, FeedID: 80, Created: false, CopiedArticles: 2},
	}}
	handler := newExploreHandlerWithStores(&fakeExploreStore{}, subscriber, time.Now)
	router := exploreTestRouter(handler)

	w := performExploreRequest(router, http.MethodPost, "/api/explore/sources/7/subscribe", nil)
	if w.Code != http.StatusOK || subscriber.lastUserID != 42 || len(subscriber.lastSourceIDs) != 1 || subscriber.lastSourceIDs[0] != 7 {
		t.Fatalf("single status=%d user=%d ids=%v body=%s", w.Code, subscriber.lastUserID, subscriber.lastSourceIDs, w.Body.String())
	}
	if w.Body.String() != "{\"feed_id\":70,\"created\":true,\"copied_articles\":3}" {
		t.Fatalf("single DTO=%s", w.Body.String())
	}

	w = performExploreRequest(router, http.MethodPost, "/api/explore/sources/subscribe-batch", map[string]any{"source_ids": []int{7, 8}})
	if w.Code != http.StatusOK || len(subscriber.lastSourceIDs) != 2 {
		t.Fatalf("batch status=%d ids=%v body=%s", w.Code, subscriber.lastSourceIDs, w.Body.String())
	}
	var body struct {
		Results []explorelogic.SubscribeResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Results) != 2 || body.Results[1].SourceID != 8 {
		t.Fatalf("batch DTO=%+v err=%v body=%s", body, err, w.Body.String())
	}
}

func TestExploreHandlerSubscribeValidatesInputAndMapsUnavailable(t *testing.T) {
	subscriber := &fakeExploreSubscriber{err: explorelogic.ErrSubscribeSourceUnavailable}
	router := exploreTestRouter(newExploreHandlerWithStores(&fakeExploreStore{}, subscriber, time.Now))

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/explore/sources/0/subscribe", nil},
		{http.MethodPost, "/api/explore/sources/subscribe-batch", map[string]any{"source_ids": []int{}}},
		{http.MethodPost, "/api/explore/sources/subscribe-batch", map[string]any{"source_ids": []int{7, 7}}},
		{http.MethodPost, "/api/explore/sources/subscribe-batch", map[string]any{"source_ids": []int{7, -1}}},
	} {
		w := performExploreRequest(router, tc.method, tc.path, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d want=400 body=%s", tc.path, w.Code, w.Body.String())
		}
	}
	w := performExploreRequest(router, http.MethodPost, "/api/explore/sources/9/subscribe", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unavailable status=%d body=%s", w.Code, w.Body.String())
	}
}

func exploreTestRouter(handler *ExploreHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", 42)
		c.Next()
	})
	router.GET("/api/explore", handler.GetExplore)
	router.GET("/api/explore/sources", handler.GetSources)
	router.GET("/api/explore/articles/:id", handler.GetArticle)
	router.POST("/api/explore/feedback", handler.CreateFeedback)
	router.DELETE("/api/explore/feedback", handler.ClearNegativeFeedback)
	router.DELETE("/api/explore/feedback/:id", handler.DeleteFeedback)
	router.PUT("/api/explore/interests", handler.ReplaceInterests)
	router.POST("/api/explore/articles/:id/events", handler.RecordArticleEvent)
	router.POST("/api/explore/sources/:id/subscribe", handler.SubscribeSource)
	router.POST("/api/explore/sources/subscribe-batch", handler.SubscribeSources)
	return router
}

func performExploreRequest(router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
