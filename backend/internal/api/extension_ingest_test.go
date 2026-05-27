package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/extension/normalizer"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
)

// ----- in-memory stubs for the extension ingest handler -----
//
// These implement the package-private extensionFeedRepo /
// extensionArticleRepo interfaces declared in extension_ingest.go, mirroring
// the bookmarkletFeedRepo / bookmarkletArticleRepo stub pattern in
// bookmarklet_pdf_test.go.

type stubExtFeedRepo struct {
	// keyed by "ownerID|feedType|sourceID"
	feeds  map[string]*model.Feed
	nextID int32
}

func newStubExtFeedRepo() *stubExtFeedRepo {
	return &stubExtFeedRepo{feeds: map[string]*model.Feed{}}
}

func (s *stubExtFeedRepo) GetOrCreateByKindAndSource(
	ownerID int, feedType, sourceID, displayName string,
) (*model.Feed, error) {
	key := fmt.Sprintf("%d|%s|%s", ownerID, feedType, sourceID)
	if f, ok := s.feeds[key]; ok {
		return f, nil
	}
	if sourceID == "" {
		return nil, fmt.Errorf("sourceID required")
	}
	id := int(atomic.AddInt32(&s.nextID, 1))
	owner := ownerID
	sid := sourceID
	name := displayName
	if name == "" {
		name = fmt.Sprintf("%s · %s", feedType, sourceID)
	}
	f := &model.Feed{
		ID:               id,
		URL:              fmt.Sprintf("extension://%s/%d/%s", feedType, ownerID, sourceID),
		Title:            name,
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          &owner,
		FeedType:         feedType,
		ProviderSourceID: &sid,
	}
	s.feeds[key] = f
	return f, nil
}

// stubExtArticleRepo is the minimum article-repo surface the extension
// ingest handler exercises: FindByOwnerAndURL + Create. It deliberately
// scopes dedupe by (ownerID, URL) — same as the real repo — by walking
// back through the feed-repo stub for the article's FeedID to recover
// its owner. Keeping this lookup live (rather than pre-populated) means
// tests don't need to interleave "create feed → register owner" by hand.
type stubExtArticleRepo struct {
	feedRepo      *stubExtFeedRepo
	byOwnerAndURL map[string]*model.Article
	created       []*model.Article
	nextID        int32
}

func newStubExtArticleRepo(feedRepo *stubExtFeedRepo) *stubExtArticleRepo {
	return &stubExtArticleRepo{
		feedRepo:      feedRepo,
		byOwnerAndURL: map[string]*model.Article{},
	}
}

// ownerOf reverses the feedRepo map to find the owner of a given feed ID.
// Callers handle the !ok case by treating the article as unowned, which
// means dedupe can't key it correctly — but the test setup always creates
// the feed before any Create, so a miss is a real bug worth surfacing.
func (s *stubExtArticleRepo) ownerOf(feedID int) (int, bool) {
	for _, f := range s.feedRepo.feeds {
		if f.ID == feedID && f.OwnerID != nil {
			return *f.OwnerID, true
		}
	}
	return 0, false
}

func (s *stubExtArticleRepo) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	key := fmt.Sprintf("%d|%s", ownerID, exactURL)
	a, ok := s.byOwnerAndURL[key]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (s *stubExtArticleRepo) Create(a *model.Article) error {
	owner, ok := s.ownerOf(a.FeedID)
	if !ok {
		return fmt.Errorf("stub: unknown ownerID for feedID=%d (feed not created via stub repo?)", a.FeedID)
	}
	a.ID = int(atomic.AddInt32(&s.nextID, 1))
	key := fmt.Sprintf("%d|%s", owner, a.URL)
	s.byOwnerAndURL[key] = a
	s.created = append(s.created, a)
	return nil
}

// newExtensionIngestTestHandler wires the handler with stub repos and a
// gin engine that injects userID=userID into the context (skipping JWT).
func newExtensionIngestTestHandler(t *testing.T, userID int) (
	*gin.Engine, *ExtensionIngestHandler, *stubExtFeedRepo, *stubExtArticleRepo,
) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	feedRepo := newStubExtFeedRepo()
	articleRepo := newStubExtArticleRepo(feedRepo)

	h := &ExtensionIngestHandler{
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
		normalizers: []normalizer.Normalizer{
			normalizer.NewTwitterNormalizer(),
		},
	}

	r := gin.New()
	// Stand-in for authHandler.AuthMiddleware: just stamp userID onto the
	// context so getUserID returns it. The JWT itself isn't under test here.
	r.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	r.POST("/api/extension/ingest", h.Ingest)
	return r, h, feedRepo, articleRepo
}

// doIngest serializes the request and invokes the test engine. Returns the
// decoded IngestResponse and the raw http status code.
func doIngest(t *testing.T, r *gin.Engine, body normalizer.IngestRequest) (normalizer.IngestResponse, int) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/extension/ingest", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp normalizer.IngestResponse
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return resp, w.Code
}

func mustTweetJSON(t *testing.T, item normalizer.TweetItem) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal tweet: %v", err)
	}
	return raw
}

// TestExtensionIngest_HappyPath sends two distinct tweets in one request
// and expects: 2 accepted, 0 skipped, a freshly-created twitter:list feed.
func TestExtensionIngest_HappyPath(t *testing.T) {
	const userID = 42
	r, _, feedRepo, articleRepo := newExtensionIngestTestHandler(t, userID)

	body := normalizer.IngestRequest{
		SourceKind: "twitter:list",
		SourceID:   "9999",
		SourceName: "Test List",
		Items: []json.RawMessage{
			mustTweetJSON(t, normalizer.TweetItem{
				ID:        "1",
				Author:    "alice",
				Text:      "first tweet",
				CreatedAt: time.Now(),
				URL:       "https://x.com/alice/status/1",
			}),
			mustTweetJSON(t, normalizer.TweetItem{
				ID:        "2",
				Author:    "alice",
				Text:      "second tweet",
				CreatedAt: time.Now(),
				URL:       "https://x.com/alice/status/2",
			}),
		},
	}

	resp, code := doIngest(t, r, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; resp=%+v", code, resp)
	}
	if resp.Accepted != 2 || resp.Skipped != 0 {
		t.Errorf("accepted=%d skipped=%d, want 2/0 (errors=%v)", resp.Accepted, resp.Skipped, resp.Errors)
	}
	if resp.FeedID <= 0 {
		t.Errorf("resp.FeedID = %d, want > 0", resp.FeedID)
	}
	if resp.FeedName != "Test List" {
		t.Errorf("resp.FeedName = %q, want %q", resp.FeedName, "Test List")
	}
	if got := len(articleRepo.created); got != 2 {
		t.Errorf("created articles = %d, want 2", got)
	}
	for _, a := range articleRepo.created {
		if a.Kind != "tweet" {
			t.Errorf("article.Kind = %q, want tweet", a.Kind)
		}
		if !a.IsClip {
			t.Errorf("article.IsClip = false, want true")
		}
	}

	// Verify the feed was auto-created with the right metadata.
	key := fmt.Sprintf("%d|%s|%s", userID, "twitter:list", "9999")
	feed, ok := feedRepo.feeds[key]
	if !ok {
		t.Fatalf("twitter:list feed for source 9999 not found; have keys=%v", mapKeys(feedRepo.feeds))
	}
	if feed.Title != "Test List" {
		t.Errorf("feed title = %q, want Test List", feed.Title)
	}
	if feed.ProviderSourceID == nil || *feed.ProviderSourceID != "9999" {
		t.Errorf("feed.ProviderSourceID = %v, want *=9999", feed.ProviderSourceID)
	}
	if feed.FeedType != "twitter:list" {
		t.Errorf("feed.FeedType = %q, want twitter:list", feed.FeedType)
	}
}

// TestExtensionIngest_Dedupe re-sends the same two tweets and expects
// 0 accepted, 2 skipped on the second call (the first creates them).
func TestExtensionIngest_Dedupe(t *testing.T) {
	const userID = 7
	r, _, _, articleRepo := newExtensionIngestTestHandler(t, userID)

	items := []json.RawMessage{
		mustTweetJSON(t, normalizer.TweetItem{
			ID:        "1",
			Author:    "bob",
			Text:      "hello",
			CreatedAt: time.Now(),
			URL:       "https://x.com/bob/status/1",
		}),
		mustTweetJSON(t, normalizer.TweetItem{
			ID:        "2",
			Author:    "bob",
			Text:      "world",
			CreatedAt: time.Now(),
			URL:       "https://x.com/bob/status/2",
		}),
	}
	body := normalizer.IngestRequest{
		SourceKind: "twitter:user",
		SourceID:   "bob",
		SourceName: "@bob",
		Items:      items,
	}

	// First send → both accepted.
	resp1, code1 := doIngest(t, r, body)
	if code1 != http.StatusOK {
		t.Fatalf("first send status = %d, want 200", code1)
	}
	if resp1.Accepted != 2 || resp1.Skipped != 0 {
		t.Fatalf("first send: accepted=%d skipped=%d, want 2/0 (errors=%v)",
			resp1.Accepted, resp1.Skipped, resp1.Errors)
	}
	if resp1.FeedID <= 0 {
		t.Errorf("resp1.FeedID = %d, want > 0", resp1.FeedID)
	}
	if resp1.FeedName != "@bob" {
		t.Errorf("resp1.FeedName = %q, want %q", resp1.FeedName, "@bob")
	}

	// Second send (identical payload) → both skipped.
	resp2, code2 := doIngest(t, r, body)
	if code2 != http.StatusOK {
		t.Fatalf("second send status = %d, want 200", code2)
	}
	if resp2.Accepted != 0 || resp2.Skipped != 2 {
		t.Errorf("second send: accepted=%d skipped=%d, want 0/2 (errors=%v)",
			resp2.Accepted, resp2.Skipped, resp2.Errors)
	}
	if resp2.FeedID != resp1.FeedID {
		t.Errorf("resp2.FeedID = %d, want stable %d", resp2.FeedID, resp1.FeedID)
	}
	if resp2.FeedName != "@bob" {
		t.Errorf("resp2.FeedName = %q, want %q", resp2.FeedName, "@bob")
	}
	// Repo should still hold only the original 2 rows.
	if got := len(articleRepo.created); got != 2 {
		t.Errorf("after dedupe re-send, created rows = %d, want 2", got)
	}
}

// mapKeys is a tiny test-only helper for clearer failure messages when a
// lookup miss happens; avoids pulling in golang.org/x/exp/maps just for tests.
func mapKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// Compile-time assertions: stubs satisfy the package-private interfaces the
// handler depends on. Catches drift if the interfaces gain methods.
var _ extensionFeedRepo = (*stubExtFeedRepo)(nil)
var _ extensionArticleRepo = (*stubExtArticleRepo)(nil)
