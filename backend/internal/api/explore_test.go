package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	eventCreated  bool
	err           error
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
	router.DELETE("/api/explore/feedback/:id", handler.DeleteFeedback)
	router.PUT("/api/explore/interests", handler.ReplaceInterests)
	router.POST("/api/explore/articles/:id/events", handler.RecordArticleEvent)
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
