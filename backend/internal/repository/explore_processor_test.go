package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

type fakeExploreSourceFetcher struct {
	mu      sync.Mutex
	request explore.SourceFetchRequest
	result  explore.SourceFetchResult
	err     error
	calls   int
	started chan struct{}
	release chan struct{}
	hook    func()
}

func (f *fakeExploreSourceFetcher) Fetch(ctx context.Context, request explore.SourceFetchRequest) (explore.SourceFetchResult, error) {
	f.mu.Lock()
	f.calls++
	f.request = request
	started, release, hook := f.started, f.release, f.hook
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return explore.SourceFetchResult{}, ctx.Err()
		}
	}
	return f.result, f.err
}

func (f *fakeExploreSourceFetcher) snapshot() (int, explore.SourceFetchRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.request
}

func TestBuildExploreSourceFetchRequestMapsEvidenceAndTaskPolicy(t *testing.T) {
	providerSuccess := time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC)
	lastSeen := providerSuccess.Add(time.Hour)
	etag, modified := `"v1"`, "Sun, 31 Aug 2026 03:00:00 GMT"
	source := ExploreCatalogSource{
		Source: model.ExploreSource{URL: "https://source.example/page", ETag: &etag, LastModified: &modified},
		Observations: []ExploreCatalogObservation{{
			ExploreSourceObservation: model.ExploreSourceObservation{ProviderID: 17, LastSeenAt: lastSeen, OccurrenceCount: 3},
			ProviderKind:             "reddit_stream", ProviderEnabled: true, ProviderLastSuccessAt: &providerSuccess,
		}},
	}

	validate := buildExploreSourceFetchRequest(source, ExploreQueueTask{TaskType: ExploreTaskValidateSource, Priority: ExplorePriorityDirectProfile})
	if validate.URL != source.Source.URL || validate.Mode != explore.SourceFetchValidate || validate.ETag != "" || validate.LastModified != "" || !validate.DirectProfile {
		t.Fatalf("validate request=%+v", validate)
	}
	if len(validate.Evidence) != 1 || validate.Evidence[0].ProviderID != 17 || validate.Evidence[0].ProviderKind != "reddit_stream" || !validate.Evidence[0].Enabled || validate.Evidence[0].ProviderLastSuccessAt != &providerSuccess || !validate.Evidence[0].LastSeenAt.Equal(lastSeen) || validate.Evidence[0].OccurrenceCount != 3 {
		t.Fatalf("evidence=%+v", validate.Evidence)
	}

	refresh := buildExploreSourceFetchRequest(source, ExploreQueueTask{TaskType: ExploreTaskRefreshArticles, Priority: ExplorePriorityRefresh})
	if refresh.Mode != explore.SourceFetchRefresh || refresh.ETag != etag || refresh.LastModified != modified || refresh.DirectProfile {
		t.Fatalf("refresh request=%+v", refresh)
	}
}

func TestExploreTaskNetworkDecisionSkipsDuplicateOrIneligibleWork(t *testing.T) {
	tests := []struct {
		name, status, task string
		want               exploreTaskNetworkDecision
	}{
		{"valid validation", model.ExploreValidationValid, ExploreTaskValidateSource, exploreTaskSkipValidated},
		{"invalid refresh", model.ExploreValidationInvalid, ExploreTaskRefreshArticles, exploreTaskInvalidateWithoutFetch},
		{"pending refresh", model.ExploreValidationPending, ExploreTaskRefreshArticles, exploreTaskInvalidateWithoutFetch},
		{"unknown task", model.ExploreValidationPending, "future_task", exploreTaskInvalidateWithoutFetch},
		{"pending validation", model.ExploreValidationPending, ExploreTaskValidateSource, exploreTaskFetch},
		{"valid refresh", model.ExploreValidationValid, ExploreTaskRefreshArticles, exploreTaskFetch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideExploreTaskNetwork(tc.task, tc.status); got != tc.want {
				t.Fatalf("decision=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestExploreTaskOutcomeRetriesInsufficientConfidenceBeforeThirdFailure(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", explore.ErrInsufficientSourceConfidence)
	for _, tc := range []struct {
		attempts int
		want     exploreTaskFailureDecision
	}{{0, exploreTaskRetry}, {1, exploreTaskRetry}, {2, exploreTaskRetry}, {3, exploreTaskInvalidate}} {
		if got := decideExploreTaskFailure(ExploreTaskValidateSource, tc.attempts, err); got != tc.want {
			t.Fatalf("attempts=%d decision=%q want=%q", tc.attempts, got, tc.want)
		}
	}
	if got := decideExploreTaskFailure(ExploreTaskRefreshArticles, 0, errors.New("network")); got != exploreTaskRetry {
		t.Fatalf("refresh retryable decision=%q", got)
	}
	if got := decideExploreTaskFailure(ExploreTaskRefreshArticles, 2, err); got != exploreTaskRetry {
		t.Fatalf("early insufficient refresh decision=%q", got)
	}
	if got := decideExploreTaskFailure(ExploreTaskRefreshArticles, 3, err); got != exploreTaskRetry {
		t.Fatalf("refresh confidence must not become terminal, decision=%q", got)
	}
}

func TestExploreTaskResultValidationKeepsRefreshPolicySeparate(t *testing.T) {
	one := explore.SourceFetchResult{FeedURL: "https://source.example/feed", Articles: []model.ExploreArticle{{Title: "one"}}}
	if err := validateExploreTaskResult(ExploreTaskValidateSource, one); err == nil {
		t.Fatal("validation accepted fewer than two articles")
	}
	if err := validateExploreTaskResult(ExploreTaskRefreshArticles, one); err != nil {
		t.Fatalf("refresh rejected a syntactically valid one-article result: %v", err)
	}
	if err := validateExploreTaskResult(ExploreTaskRefreshArticles, explore.SourceFetchResult{}); err == nil {
		t.Fatal("refresh accepted a missing feed URL")
	}
}

func TestExploreTaskProcessorCorrectOwnerPersistsSuccessAndRefresh(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
	sourceID := insertProcessorSource(t, db, "https://processor-success.example/page", model.ExploreValidationPending)
	insertProcessorEvidence(t, db, sourceID, checkedAt)
	task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
	fetcher := &fakeExploreSourceFetcher{result: processorSuccessResult(checkedAt, "https://processor-success.example/feed")}

	err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	assertProcessorTaskStatus(t, db, task.ID, model.ExploreFetchTaskDone)
	var canonical, status string
	if err := db.QueryRow(`SELECT normalized_url,validation_status FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&canonical, &status); err != nil {
		t.Fatal(err)
	}
	if canonical != "https://processor-success.example/feed" || status != model.ExploreValidationValid {
		t.Fatalf("canonical=%q status=%q", canonical, status)
	}
	var articleCount, refreshCount int
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, sourceID).Scan(&articleCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND task_type=$2 AND status='pending' AND priority=$3`, sourceID, ExploreTaskRefreshArticles, ExplorePriorityRefresh).Scan(&refreshCount); err != nil {
		t.Fatal(err)
	}
	if articleCount != 2 || refreshCount != 1 {
		t.Fatalf("articles=%d refresh=%d", articleCount, refreshCount)
	}
	_, request := fetcher.snapshot()
	if request.ETag != "" || request.LastModified != "" || request.DirectProfile {
		t.Fatalf("validate request=%+v", request)
	}
}

func TestExploreTaskProcessorWrongOrExpiredLeaseRollsBackAllMutations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		owner     string
		expireNow bool
	}{{"wrong-owner", "worker-b", false}, {"expired-owner", "worker-a", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
			originalURL := "https://processor-fenced-" + tc.name + ".example/page"
			targetURL := "https://processor-fenced-target-" + tc.name + ".example/feed"
			sourceID := insertProcessorSource(t, db, originalURL, model.ExploreValidationPending)
			insertProcessorEvidence(t, db, sourceID, checkedAt)
			task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
			if tc.expireNow {
				if _, err := db.Exec(`UPDATE explore_fetch_queue SET lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE id=$1`, task.ID); err != nil {
					t.Fatal(err)
				}
			}
			fetcher := &fakeExploreSourceFetcher{result: processorSuccessResult(checkedAt, targetURL)}
			err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, tc.owner)
			if !errors.Is(err, ErrExploreLeaseNotHeld) {
				t.Fatalf("err=%v", err)
			}
			var sourceURL, status string
			if err := db.QueryRow(`SELECT normalized_url,validation_status FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&sourceURL, &status); err != nil {
				t.Fatal(err)
			}
			var articles, refresh int
			if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, sourceID).Scan(&articles); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND task_type=$2`, sourceID, ExploreTaskRefreshArticles).Scan(&refresh); err != nil {
				t.Fatal(err)
			}
			if sourceURL != originalURL || status != model.ExploreValidationPending || articles != 0 || refresh != 0 {
				t.Fatalf("rollback sourceURL=%q status=%q articles=%d refresh=%d", sourceURL, status, articles, refresh)
			}
		})
	}
}

func TestExploreTaskProcessorPersistsFailure304MergeAndSkipOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		taskType   string
		attempts   int
		result     explore.SourceFetchResult
		fetchErr   error
		wantTask   string
		wantSource string
		wantCalls  int
	}{
		{"retryable-refresh", model.ExploreValidationValid, ExploreTaskRefreshArticles, 0, explore.SourceFetchResult{}, errors.New("temporary"), model.ExploreFetchTaskPending, model.ExploreValidationValid, 1},
		{"early-insufficient-validation", model.ExploreValidationPending, ExploreTaskValidateSource, 2, explore.SourceFetchResult{}, fmt.Errorf("wrapped: %w", explore.ErrInsufficientSourceConfidence), model.ExploreFetchTaskPending, model.ExploreValidationPending, 1},
		{"third-insufficient-validation", model.ExploreValidationPending, ExploreTaskValidateSource, 3, explore.SourceFetchResult{}, fmt.Errorf("wrapped: %w", explore.ErrInsufficientSourceConfidence), model.ExploreFetchTaskInvalid, model.ExploreValidationInvalid, 1},
		{"terminal-refresh", model.ExploreValidationValid, ExploreTaskRefreshArticles, 0, explore.SourceFetchResult{}, httpx.ErrResponseTooLarge, model.ExploreFetchTaskInvalid, model.ExploreValidationInvalid, 1},
		{"refresh-not-modified", model.ExploreValidationValid, ExploreTaskRefreshArticles, 0, explore.SourceFetchResult{NotModified: true}, nil, model.ExploreFetchTaskDone, model.ExploreValidationValid, 1},
		{"validation-not-modified", model.ExploreValidationPending, ExploreTaskValidateSource, 0, explore.SourceFetchResult{NotModified: true}, nil, model.ExploreFetchTaskInvalid, model.ExploreValidationInvalid, 1},
		{"defensive-short-200", model.ExploreValidationPending, ExploreTaskValidateSource, 0, explore.SourceFetchResult{FeedURL: "https://processor-case.example/feed", Articles: []model.ExploreArticle{{Title: "one"}}}, nil, model.ExploreFetchTaskInvalid, model.ExploreValidationInvalid, 1},
		{"valid-validation-skips", model.ExploreValidationValid, ExploreTaskValidateSource, 0, explore.SourceFetchResult{}, nil, model.ExploreFetchTaskDone, model.ExploreValidationValid, 0},
		{"invalid-refresh-skips", model.ExploreValidationInvalid, ExploreTaskRefreshArticles, 0, explore.SourceFetchResult{}, nil, model.ExploreFetchTaskInvalid, model.ExploreValidationInvalid, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
			sourceID := insertProcessorSource(t, db, "https://processor-"+tc.name+".example/feed", tc.status)
			insertProcessorEvidence(t, db, sourceID, checkedAt)
			if tc.name == "early-insufficient-validation" || tc.name == "third-insufficient-validation" {
				if _, err := db.Exec(`UPDATE explore_registry_providers SET last_success_at=NULL WHERE provider_key=$1`, fmt.Sprintf("processor-provider-%d", sourceID)); err != nil {
					t.Fatal(err)
				}
			}
			task := leaseProcessorTask(t, db, sourceID, tc.taskType, ExplorePriorityStructuredProvider, "worker-a")
			if tc.attempts > 0 {
				if _, err := db.Exec(`UPDATE explore_fetch_queue SET attempts=$2 WHERE id=$1`, task.ID, tc.attempts); err != nil {
					t.Fatal(err)
				}
				task.Attempts = tc.attempts
			}
			fetcher := &fakeExploreSourceFetcher{result: tc.result, err: tc.fetchErr}
			if err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a"); err != nil {
				t.Fatal(err)
			}
			assertProcessorTaskStatus(t, db, task.ID, tc.wantTask)
			var status string
			var health sql.NullFloat64
			if err := db.QueryRow(`SELECT validation_status,health_score FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &health); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantSource {
				t.Fatalf("source status=%q want=%q", status, tc.wantSource)
			}
			calls, request := fetcher.snapshot()
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tc.wantCalls)
			}
			if tc.taskType == ExploreTaskRefreshArticles && tc.wantCalls == 1 && request.URL != "" {
				// A valid refresh must pass saved validators; fresh fixture sources
				// have none, so both values remain empty.
				if request.ETag != "" || request.LastModified != "" {
					t.Fatalf("unexpected validators request=%+v", request)
				}
			}
			if tc.name == "retryable-refresh" && (!health.Valid || health.Float64 >= 1) {
				t.Fatalf("health=%v", health)
			}
			if tc.name == "valid-validation-skips" {
				var refresh int
				if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND task_type=$2 AND status='pending'`, sourceID, ExploreTaskRefreshArticles).Scan(&refresh); err != nil {
					t.Fatal(err)
				}
				if refresh != 1 {
					t.Fatalf("valid validation refresh count=%d", refresh)
				}
			}
		})
	}

	t.Run("merge-loser-into-target", func(t *testing.T) {
		db, cleanup := testdb.New(t)
		defer cleanup()
		checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
		loserID := insertProcessorSource(t, db, "https://processor-loser.example/page", model.ExploreValidationPending)
		targetID := insertProcessorSource(t, db, "https://processor-target.example/feed", model.ExploreValidationValid)
		insertProcessorEvidence(t, db, loserID, checkedAt)
		task := leaseProcessorTask(t, db, loserID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
		fetcher := &fakeExploreSourceFetcher{result: processorSuccessResult(checkedAt, "https://processor-target.example/feed")}
		if err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a"); err != nil {
			t.Fatal(err)
		}
		var loserStatus, targetStatus string
		if err := db.QueryRow(`SELECT validation_status FROM recommended_feeds WHERE id=$1`, loserID).Scan(&loserStatus); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT validation_status FROM recommended_feeds WHERE id=$1`, targetID).Scan(&targetStatus); err != nil {
			t.Fatal(err)
		}
		var articles, refresh int
		if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, targetID).Scan(&articles); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND task_type=$2 AND status='pending'`, targetID, ExploreTaskRefreshArticles).Scan(&refresh); err != nil {
			t.Fatal(err)
		}
		if loserStatus != model.ExploreValidationInvalid || targetStatus != model.ExploreValidationValid || articles != 2 || refresh != 1 {
			t.Fatalf("loser=%q target=%q articles=%d refresh=%d", loserStatus, targetStatus, articles, refresh)
		}
	})

	t.Run("final-insufficient-races-with-provider-success", func(t *testing.T) {
		db, cleanup := testdb.New(t)
		defer cleanup()
		checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
		sourceID := insertProcessorSource(t, db, "https://processor-confidence-race.example/feed", model.ExploreValidationPending)
		insertProcessorEvidence(t, db, sourceID, checkedAt)
		if _, err := db.Exec(`UPDATE explore_registry_providers SET last_success_at=NULL WHERE provider_key=$1`, fmt.Sprintf("processor-provider-%d", sourceID)); err != nil {
			t.Fatal(err)
		}
		task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
		if _, err := db.Exec(`UPDATE explore_fetch_queue SET attempts=3 WHERE id=$1`, task.ID); err != nil {
			t.Fatal(err)
		}
		task.Attempts = 3
		fetcher := &fakeExploreSourceFetcher{err: fmt.Errorf("wrapped: %w", explore.ErrInsufficientSourceConfidence), hook: func() {
			if _, err := db.Exec(`UPDATE explore_registry_providers SET last_success_at=$2 WHERE provider_key=$1`, fmt.Sprintf("processor-provider-%d", sourceID), checkedAt); err != nil {
				panic(err)
			}
		}}
		if err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a"); err != nil {
			t.Fatal(err)
		}
		assertProcessorTaskStatus(t, db, task.ID, model.ExploreFetchTaskPending)
		var status string
		if err := db.QueryRow(`SELECT validation_status FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != model.ExploreValidationPending {
			t.Fatalf("provider-success race invalidated source: %q", status)
		}
	})
}

func TestExploreTaskProcessorWrongOwnerRetryRollsBackHealth(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
	sourceID := insertProcessorSource(t, db, "https://processor-retry-fenced.example/feed", model.ExploreValidationValid)
	insertProcessorEvidence(t, db, sourceID, checkedAt)
	task := leaseProcessorTask(t, db, sourceID, ExploreTaskRefreshArticles, ExplorePriorityRefresh, "worker-a")
	fetcher := &fakeExploreSourceFetcher{err: errors.New("temporary")}
	err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-b")
	if !errors.Is(err, ErrExploreLeaseNotHeld) {
		t.Fatalf("err=%v", err)
	}
	var health float64
	if err := db.QueryRow(`SELECT health_score FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&health); err != nil {
		t.Fatal(err)
	}
	if health != 1 {
		t.Fatalf("fenced retry committed health=%v", health)
	}
	assertProcessorTaskStatus(t, db, task.ID, model.ExploreFetchTaskLeased)
}

func TestExploreTaskProcessorDoesNotOpenTransactionDuringNetwork(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(3)
	checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
	sourceID := insertProcessorSource(t, db, "https://processor-block.example/feed", model.ExploreValidationPending)
	insertProcessorEvidence(t, db, sourceID, checkedAt)
	task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
	fetcher := &fakeExploreSourceFetcher{started: make(chan struct{}), release: make(chan struct{}), result: processorSuccessResult(checkedAt, "https://processor-block.example/feed")}
	done := make(chan error, 1)
	go func() {
		done <- NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a")
	}()
	<-fetcher.started
	if stats := db.Stats(); stats.InUse != 0 {
		close(fetcher.release)
		t.Fatalf("network wait holds %d database connections", stats.InUse)
	}
	var one int
	if err := db.QueryRow(`SELECT 1`).Scan(&one); err != nil || one != 1 {
		close(fetcher.release)
		t.Fatalf("independent connection unavailable one=%d err=%v", one, err)
	}
	close(fetcher.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestExploreTaskProcessorMissingSourceAndUnknownTaskUseFencedInvalidate(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mutateTask   func(*ExploreQueueTask)
		owner        string
		wantStatus   string
		wantLeaseErr bool
	}{
		{"missing-source", func(task *ExploreQueueTask) { task.SourceID += 100000 }, "worker-a", model.ExploreFetchTaskInvalid, false},
		{"missing-source-wrong-owner", func(task *ExploreQueueTask) { task.SourceID += 100000 }, "worker-b", model.ExploreFetchTaskLeased, true},
		{"unknown-task", func(task *ExploreQueueTask) { task.TaskType = "future_task" }, "worker-a", model.ExploreFetchTaskInvalid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
			sourceID := insertProcessorSource(t, db, "https://processor-"+tc.name+".example/feed", model.ExploreValidationPending)
			task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
			tc.mutateTask(&task)
			fetcher := &fakeExploreSourceFetcher{}
			err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, tc.owner)
			if tc.wantLeaseErr != errors.Is(err, ErrExploreLeaseNotHeld) {
				t.Fatalf("err=%v wantLeaseErr=%v", err, tc.wantLeaseErr)
			}
			assertProcessorTaskStatus(t, db, task.ID, tc.wantStatus)
			calls, _ := fetcher.snapshot()
			if calls != 0 {
				t.Fatalf("fetch calls=%d", calls)
			}
		})
	}
}

func TestExploreTaskProcessorSourceDeletedDuringFetchUsesFencedTransition(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	checkedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
	sourceID := insertProcessorSource(t, db, "https://processor-deleted.example/feed", model.ExploreValidationPending)
	insertProcessorEvidence(t, db, sourceID, checkedAt)
	task := leaseProcessorTask(t, db, sourceID, ExploreTaskValidateSource, ExplorePriorityStructuredProvider, "worker-a")
	fetcher := &fakeExploreSourceFetcher{result: processorSuccessResult(checkedAt, "https://processor-deleted.example/feed"), hook: func() {
		if _, err := db.Exec(`DELETE FROM recommended_feeds WHERE id=$1`, sourceID); err != nil {
			panic(err)
		}
	}}
	err := NewExploreTaskProcessor(db, fetcher, func() time.Time { return checkedAt }).Process(context.Background(), task, "worker-a")
	if !errors.Is(err, ErrExploreLeaseNotHeld) {
		t.Fatalf("deleted source should lose cascaded lease, err=%v", err)
	}
}

func processorSuccessResult(checkedAt time.Time, feedURL string) explore.SourceFetchResult {
	return explore.SourceFetchResult{FeedURL: feedURL, ETag: `"new"`, LastModified: "new-modified", Articles: []model.ExploreArticle{
		{URL: "https://articles.example/one", NormalizedURL: "https://articles.example/one", Title: "one", FetchedAt: checkedAt.Add(-time.Hour)},
		{URL: "https://articles.example/two", NormalizedURL: "https://articles.example/two", Title: "two"},
	}}
}

func insertProcessorSource(t *testing.T, db *sql.DB, rawURL, status string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds (url,title,category,language,normalized_url,validation_status,health_score) VALUES ($1,$1,'test','en',$1,$2,CASE WHEN $2='valid' THEN 1 ELSE NULL END) RETURNING id`, rawURL, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertProcessorEvidence(t *testing.T, db *sql.DB, sourceID int, now time.Time) {
	t.Helper()
	var providerID int
	key := fmt.Sprintf("processor-provider-%d", sourceID)
	if err := db.QueryRow(`INSERT INTO explore_registry_providers (provider_key,provider_kind,endpoint,last_success_at) VALUES ($1,'opml',$2,$3) RETURNING id`, key, "https://providers.example/"+key, now).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations (provider_id,source_id,external_key,last_seen_at,occurrence_count) VALUES ($1,$2,$3,$4,2)`, providerID, sourceID, key, now); err != nil {
		t.Fatal(err)
	}
}

func leaseProcessorTask(t *testing.T, db *sql.DB, sourceID int, taskType string, priority int, owner string) ExploreQueueTask {
	t.Helper()
	repo := NewExploreQueueRepository(db)
	if _, err := repo.Enqueue(sourceID, taskType, priority); err != nil {
		t.Fatal(err)
	}
	_, tasks, err := repo.ClaimRun(time.Now(), owner, time.Hour, 1)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("claim tasks=%d err=%v", len(tasks), err)
	}
	return tasks[0]
}

func assertProcessorTaskStatus(t *testing.T, db *sql.DB, taskID int, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM explore_fetch_queue WHERE id=$1`, taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("task status=%q want=%q", got, want)
	}
}
