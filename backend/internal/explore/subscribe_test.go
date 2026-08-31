package explore

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestSubscribePromotesCandidateArticlesWithoutUserSideEffects(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-copy")
	sourceID := seedSubscribeSource(t, db, userID, "https://candidate.example/feed", "Candidate", "valid", now)
	published := now.Add(-2 * time.Hour)
	var candidateArticleID int
	if err := db.QueryRow(`
		INSERT INTO explore_articles (source_id,url,normalized_url,title,content,excerpt,published_at,fetched_at)
		VALUES ($1,'https://candidate.example/post','https://candidate.example/post','Post','body','do not copy',$2,$3)
		RETURNING id`, sourceID, published, now.Add(-time.Hour)).Scan(&candidateArticleID); err != nil {
		t.Fatal(err)
	}

	result, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != sourceID || result.FeedID <= 0 || !result.Created || result.CopiedArticles != 1 {
		t.Fatalf("result=%+v", result)
	}
	var title, url, content, summaryBrief, summaryDetailed string
	var gotPublished *time.Time
	var gotFetched time.Time
	var tags []byte
	if err := db.QueryRow(`
		SELECT title,url,content,published_at,fetched_at,
		       COALESCE(summary_brief,''),COALESCE(summary_detailed,''),COALESCE(tags,'{}')::text
		FROM articles WHERE feed_id=$1`, result.FeedID).Scan(
		&title, &url, &content, &gotPublished, &gotFetched, &summaryBrief, &summaryDetailed, &tags,
	); err != nil {
		t.Fatal(err)
	}
	if title != "Post" || url != "https://candidate.example/post" || content != "body" || gotPublished == nil || !gotPublished.Equal(published) || !gotFetched.Equal(now.Add(-time.Hour)) {
		t.Fatalf("copied title=%q url=%q content=%q published=%v fetched=%v", title, url, content, gotPublished, gotFetched)
	}
	if summaryBrief != "" || summaryDetailed != "" || string(tags) != "{}" {
		t.Fatalf("derived data copied summary=%q/%q tags=%s", summaryBrief, summaryDetailed, tags)
	}
	for table := range map[string]struct{}{
		"explore_article_events": {}, "explore_feedback": {}, "user_preferences": {},
		"reading_progress": {}, "article_user_tags": {},
	} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("side effect table=%s count=%d err=%v", table, count, err)
		}
	}

	again, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, sourceID)
	if err != nil || again.FeedID != result.FeedID || again.Created || again.CopiedArticles != 1 {
		t.Fatalf("idempotent result=%+v err=%v", again, err)
	}
	_ = candidateArticleID
}

func TestSubscribeRequiresRecentDoneSnapshotAndValidSource(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-boundary")
	otherUserID := seedSubscribeUser(t, db, "subscribe-other")
	missingID := seedSubscribeSource(t, db, otherUserID, "https://missing.example/feed", "Missing", "valid", now)
	invalidID := seedSubscribeSource(t, db, userID, "https://invalid.example/feed", "Invalid", "invalid", now)
	staleID := seedSubscribeSource(t, db, userID, "https://stale.example/feed", "Stale", "valid", now.Add(-31*24*time.Hour))
	service := NewSubscribeService(db, func() time.Time { return now })

	for _, sourceID := range []int{missingID, invalidID, staleID} {
		if _, err := service.SubscribeOne(userID, sourceID); !errors.Is(err, ErrSubscribeSourceUnavailable) {
			t.Fatalf("source=%d err=%v", sourceID, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1`, userID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("created unauthorized feeds count=%d err=%v", count, err)
	}
}

func TestSubscribeBatchCommitsAllOrRollsBackAll(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-batch")
	firstID := seedSubscribeSource(t, db, userID, "https://batch-one.example/feed", "One", "valid", now)
	secondID := seedSubscribeSource(t, db, userID, "https://batch-two.example/feed", "Two", "valid", now)
	invalidID := seedSubscribeSource(t, db, userID, "https://batch-bad.example/feed", "Bad", "invalid", now)
	service := NewSubscribeService(db, func() time.Time { return now })

	if _, err := service.Subscribe(userID, []int{firstID, invalidID}); !errors.Is(err, ErrSubscribeSourceUnavailable) {
		t.Fatalf("mixed batch err=%v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1`, userID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial batch count=%d err=%v", count, err)
	}
	results, err := service.Subscribe(userID, []int{firstID, secondID})
	if err != nil || len(results) != 2 || !results[0].Created || !results[1].Created {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1`, userID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("committed batch count=%d err=%v", count, err)
	}
}

func TestSubscribeReusesSharedAndConcurrentCallsConverge(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(12)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-concurrent")
	sharedSourceID := seedSubscribeSource(t, db, userID, "https://subscribe-shared.example/feed", "Shared Candidate", "valid", now)
	var sharedFeedID int
	if err := db.QueryRow(`INSERT INTO feeds (url,title) VALUES ('https://subscribe-shared.example/feed','Shared') RETURNING id`).Scan(&sharedFeedID); err != nil {
		t.Fatal(err)
	}
	shared, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, sharedSourceID)
	if err != nil || shared.Created || shared.FeedID != sharedFeedID {
		t.Fatalf("shared=%+v err=%v", shared, err)
	}

	ownedSourceID := seedSubscribeSource(t, db, userID, "https://subscribe-race.example/feed", "Race", "valid", now)
	const callers = 8
	start := make(chan struct{})
	results := make(chan SubscribeResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, ownedSourceID)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent subscribe: %v", err)
		}
	}
	wantID := 0
	created := 0
	for result := range results {
		if wantID == 0 {
			wantID = result.FeedID
		}
		if result.FeedID != wantID {
			t.Fatalf("feed ids diverged got=%d want=%d", result.FeedID, wantID)
		}
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created responses=%d want=1", created)
	}
}

func seedSubscribeUser(t *testing.T, db *sql.DB, username string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`INSERT INTO users (username,password_hash) VALUES ($1,'x') RETURNING id`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedSubscribeSource(t *testing.T, db *sql.DB, userID int, url, title, status string, completedAt time.Time) int {
	t.Helper()
	var sourceID int
	if err := db.QueryRow(`
		INSERT INTO recommended_feeds
		(url,title,category,language,feed_type,normalized_url,validation_status,is_broken)
		VALUES ($1,$2,'technology','en','rss',$1,$3,false) RETURNING id`, url, title, status).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	var batchID, rank int
	if err := db.QueryRow(`
		INSERT INTO explore_batches (user_id,slot_at,status,source_count,completed_at)
		VALUES ($1,$2,'done',1,$2)
		ON CONFLICT (user_id,slot_at) DO UPDATE
		SET source_count=explore_batches.source_count+1
		RETURNING id,source_count`, userID, completedAt).Scan(&batchID, &rank); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO explore_batch_sources (user_id,batch_id,source_id,rank,score)
		VALUES ($1,$2,$3,$4,1)`, userID, batchID, sourceID, rank); err != nil {
		t.Fatal(err)
	}
	return sourceID
}
