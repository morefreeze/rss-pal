package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

type coldLoaderCtx map[string]any

func (ctx coldLoaderCtx) Get(key string) (any, bool) { value, ok := ctx[key]; return value, ok }

type fakeColdStarter struct {
	calls  int
	userID int
	err    error
}

func (starter *fakeColdStarter) Ensure(userID int, _ time.Time) error {
	starter.calls++
	starter.userID = userID
	return starter.err
}

func TestExploreHandlerEnsuresColdSnapshotForPageAndDrawerWithoutBlockingOnError(t *testing.T) {
	store := &fakeExploreStore{page: &repository.ExplorePage{}, sources: []repository.ExploreSourceItem{}}
	starter := &fakeColdStarter{err: errors.New("candidate refresh racing")}
	handler := newExploreHandlerWithStore(store, time.Now)
	handler.coldStartFor = func(*gin.Context) exploreColdStarter { return starter }
	router := exploreTestRouter(handler)
	for _, path := range []string{"/api/explore", "/api/explore/sources"} {
		response := performExploreRequest(router, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if starter.calls != 2 || starter.userID != 42 {
		t.Fatalf("cold starts=%d user=%d", starter.calls, starter.userID)
	}
}

type fakeColdRankLoader struct {
	candidates    []explorelogic.RankCandidate
	subscriptions []explorelogic.SubscriptionSignalInput
	feedback      []explorelogic.ExplicitFeedbackInput
}

func (loader *fakeColdRankLoader) LoadColdCandidates(time.Time) ([]explorelogic.RankCandidate, error) {
	return loader.candidates, nil
}
func (loader *fakeColdRankLoader) LoadColdFeedback(int) ([]explorelogic.ExplicitFeedbackInput, error) {
	return loader.feedback, nil
}
func (loader *fakeColdRankLoader) LoadColdSubscriptions(int) ([]explorelogic.SubscriptionSignalInput, error) {
	return loader.subscriptions, nil
}

type fakeColdSnapshotStore struct {
	latest *model.ExploreBatch
	claim  repository.ExploreSnapshotClaim
	owned  bool
	values []repository.ExploreSnapshotSourceInput
	failed bool
}

func (store *fakeColdSnapshotStore) LatestDone(int) (*model.ExploreBatch, []model.ExploreBatchSource, error) {
	if store.latest == nil {
		return nil, nil, sql.ErrNoRows
	}
	return store.latest, nil, nil
}
func (store *fakeColdSnapshotStore) Claim(int, time.Time, time.Time, time.Duration) (*repository.ExploreSnapshotClaim, bool, error) {
	return &store.claim, store.owned, nil
}
func (store *fakeColdSnapshotStore) Publish(_ int, _ repository.ExploreSnapshotGenerationToken, values []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error) {
	store.values = append([]repository.ExploreSnapshotSourceInput(nil), values...)
	return &model.ExploreBatch{ID: store.claim.Batch.ID, Status: model.ExploreBatchDone}, nil
}
func (store *fakeColdSnapshotStore) Fail(int, repository.ExploreSnapshotGenerationToken, error) error {
	store.failed = true
	return nil
}

func TestExploreColdStartPublishesDeterministicUserScopedFallback(t *testing.T) {
	now := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	store := &fakeColdSnapshotStore{owned: true, claim: repository.ExploreSnapshotClaim{
		Batch: model.ExploreBatch{ID: 77, UserID: 5, SlotAt: repository.ExploreColdStartSlotAt},
	}}
	loader := &fakeColdRankLoader{
		subscriptions: []explorelogic.SubscriptionSignalInput{{SourceID: 3, Domain: "subscribed.example"}},
		feedback:      []explorelogic.ExplicitFeedbackInput{{SourceID: 2, Type: explorelogic.FeedbackHideSource}},
		candidates: []explorelogic.RankCandidate{
			{SourceID: 2, Title: "hidden", ValidationStatus: model.ExploreValidationValid, HealthScore: 1, Articles: []explorelogic.RankArticle{{ID: 20, FetchedAt: now}}},
			{SourceID: 3, Title: "subscribed source", Domain: "another.example", ValidationStatus: model.ExploreValidationValid, HealthScore: 1, Articles: []explorelogic.RankArticle{{ID: 30, FetchedAt: now}}},
			{SourceID: 4, Title: "subscribed domain", Domain: "subscribed.example", ValidationStatus: model.ExploreValidationValid, HealthScore: 1, Articles: []explorelogic.RankArticle{{ID: 40, FetchedAt: now}}},
			{SourceID: 1, Title: "visible", Topic: "tech", Provider: "directory", ValidationStatus: model.ExploreValidationValid, HealthScore: .9, Articles: []explorelogic.RankArticle{{ID: 10, FetchedAt: now}}},
		},
	}
	service := NewExploreColdStartService(store, loader, log.New(io.Discard, "", 0))
	if err := service.Ensure(5, now); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(store.values) != 1 || store.values[0].SourceID != 1 {
		t.Fatalf("published=%+v", store.values)
	}
}

func TestExploreColdStartNoCandidatesLeavesGeneratingClaimWithoutPublishing(t *testing.T) {
	now := time.Now()
	store := &fakeColdSnapshotStore{owned: true, claim: repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 77}}}
	service := NewExploreColdStartService(store, &fakeColdRankLoader{}, log.New(io.Discard, "", 0))
	if err := service.Ensure(5, now); !errors.Is(err, ErrExploreColdStartPending) {
		t.Fatalf("Ensure error=%v", err)
	}
	if len(store.values) != 0 || store.failed {
		t.Fatalf("empty cold fallback was published/failed: values=%v failed=%t", store.values, store.failed)
	}
}

func TestExploreColdStartEmptyClaimRetriesPromptly(t *testing.T) {
	if coldStartStaleAfter > time.Minute {
		t.Fatalf("cold pending retry=%s, want at most one minute", coldStartStaleAfter)
	}
}

func TestExploreColdSubscriptionsUseRequestRLSTransaction(t *testing.T) {
	privDB, schema, cleanupSchema := testdb.NewWithSchema(t)
	defer cleanupSchema()
	appDB, cleanupApp := testdb.NewAsApp(t, schema)
	defer cleanupApp()

	var userID, otherUserID int
	if err := privDB.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('cold-rls-a','x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := privDB.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('cold-rls-b','x') RETURNING id`).Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := privDB.Exec(`
		INSERT INTO feeds(url,title,owner_id) VALUES
			('https://shared.example/feed','shared',NULL),
			('https://mine.example/feed','mine',$1),
			('https://other.example/feed','other',$2)`, userID, otherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := privDB.Exec(`
		INSERT INTO recommended_feeds(url,normalized_url,title,category,language,validation_status) VALUES
			('https://shared.example/feed','https://shared.example/feed','shared','tech','en','valid'),
			('https://mine.example/feed','https://mine.example/feed','mine','tech','en','valid'),
			('https://other.example/feed','https://other.example/feed','other','tech','en','valid')`); err != nil {
		t.Fatal(err)
	}

	tx, err := appDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('app.user_id',$1,true)`, userID); err != nil {
		t.Fatal(err)
	}
	loader := NewSQLExploreColdRankLoader(appDB).WithCtx(coldLoaderCtx{ctxkey.Tx: repository.Querier(tx)})
	subscriptions, err := loader.LoadColdSubscriptions(userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 2 {
		t.Fatalf("subscriptions=%+v, want shared and owned only", subscriptions)
	}
	got := map[string]int{}
	for _, subscription := range subscriptions {
		got[subscription.Domain] = subscription.SourceID
	}
	if got["shared.example"] == 0 || got["mine.example"] == 0 || got["other.example"] != 0 {
		t.Fatalf("subscriptions by domain=%v", got)
	}
}
