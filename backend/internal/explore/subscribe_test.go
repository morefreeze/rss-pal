package explore

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestCopyExploreArticlesSQLMatchesArticleNaturalKeyAndRefreshesDerivedData(t *testing.T) {
	sqlText := copyExploreArticleCandidatesSQL + copyExploreArticleUpsertSQL +
		copyExploreSharedArticleUpdateSQL + copyExploreSharedArticleInsertSQL
	for _, required := range []string{
		"ON CONFLICT (feed_id,url) WHERE parent_article_id IS NULL AND NOT is_clip",
		"published_at IS NOT NULL",
		"app_user_shared_floor()",
		"summary_brief=CASE WHEN",
		"summary_detailed=CASE WHEN",
		"word_count=EXCLUDED.word_count",
		"reading_minutes=EXCLUDED.reading_minutes",
		"DO NOTHING",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("copy SQL missing %q", required)
		}
	}
	if got := strings.Count(copyExploreArticleUpsertSQL, "articles.content IS DISTINCT FROM EXCLUDED.content"); got != 2 {
		t.Errorf("owned summary invalidation content checks=%d want=2", got)
	}
	if got := strings.Count(copyExploreSharedArticleUpdateSQL, "articles.content IS DISTINCT FROM $4"); got != 2 {
		t.Errorf("shared summary invalidation content checks=%d want=2", got)
	}
	for _, forbidden := range []string{
		"(articles.title,articles.content,articles.published_at,articles.fetched_at)",
		"(EXCLUDED.title,EXCLUDED.content,EXCLUDED.published_at,EXCLUDED.fetched_at)",
	} {
		if strings.Contains(copyExploreArticleUpsertSQL+copyExploreSharedArticleUpdateSQL, forbidden) {
			t.Errorf("summary invalidation still depends on non-content tuple %q", forbidden)
		}
	}
}

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
	var wordCount, readingMinutes int
	var tags []byte
	if err := db.QueryRow(`
		SELECT title,url,content,published_at,fetched_at,
		       COALESCE(summary_brief,''),COALESCE(summary_detailed,''),COALESCE(tags,'{}')::text,
		       word_count,reading_minutes
		FROM articles WHERE feed_id=$1`, result.FeedID).Scan(
		&title, &url, &content, &gotPublished, &gotFetched, &summaryBrief, &summaryDetailed, &tags,
		&wordCount, &readingMinutes,
	); err != nil {
		t.Fatal(err)
	}
	if title != "Post" || url != "https://candidate.example/post" || content != "body" || gotPublished == nil || !gotPublished.Equal(published) || !gotFetched.Equal(now.Add(-time.Hour)) {
		t.Fatalf("copied title=%q url=%q content=%q published=%v fetched=%v", title, url, content, gotPublished, gotFetched)
	}
	if summaryBrief != "" || summaryDetailed != "" || string(tags) != "{}" {
		t.Fatalf("derived data copied summary=%q/%q tags=%s", summaryBrief, summaryDetailed, tags)
	}
	if wordCount != 1 || readingMinutes != 1 {
		t.Fatalf("metrics=%d/%d want=1/1", wordCount, readingMinutes)
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

func TestSubscribeSharedFeedCopiesOnlyArticlesVisibleAtUserFloor(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-shared-floor")
	floor := now.Add(-7 * 24 * time.Hour)
	if _, err := db.Exec(`UPDATE users SET shared_visible_from=$1 WHERE id=$2`, floor, userID); err != nil {
		t.Fatal(err)
	}
	sourceID := seedSubscribeSource(t, db, userID, "https://shared-floor.example/feed", "Shared", "valid", now)
	var sharedFeedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url,title) VALUES ('https://shared-floor.example/feed','Shared') RETURNING id`,
	).Scan(&sharedFeedID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO explore_articles (source_id,url,normalized_url,title,content,published_at,fetched_at)
		VALUES
			($1,'https://shared-floor.example/recent','https://shared-floor.example/recent','Recent','recent body',$2,$3),
			($1,'https://shared-floor.example/conflict','https://shared-floor.example/conflict','Candidate conflict','candidate body',$2,$3),
			($1,'https://shared-floor.example/old','https://shared-floor.example/old','Old','old body',$4,$3),
			($1,'https://shared-floor.example/undated','https://shared-floor.example/undated','Undated','undated body',NULL,$3)`,
		sourceID, floor, now, floor.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO articles (feed_id,url,title,content,published_at)
		VALUES ($1,'https://shared-floor.example/conflict','Existing hidden conflict','existing body',$2)`,
		sharedFeedID, floor.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	result, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.FeedID != sharedFeedID || result.CopiedArticles != 1 {
		t.Fatalf("result=%+v", result)
	}
	rows, err := db.Query(`SELECT url,title,content FROM articles WHERE feed_id=$1 ORDER BY url`, sharedFeedID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var url, title, content string
		if err := rows.Scan(&url, &title, &content); err != nil {
			t.Fatal(err)
		}
		switch url {
		case "https://shared-floor.example/conflict":
			if title != "Existing hidden conflict" || content != "existing body" {
				t.Fatalf("hidden conflict was updated title=%q content=%q", title, content)
			}
		case "https://shared-floor.example/recent":
			if title != "Recent" || content != "recent body" {
				t.Fatalf("recent copy title=%q content=%q", title, content)
			}
		default:
			t.Fatalf("unexpected shared article %q", url)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var sharedCount int
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=$1`, sharedFeedID).Scan(&sharedCount); err != nil {
		t.Fatal(err)
	}
	if sharedCount != 2 {
		t.Fatalf("shared article count=%d want=2", sharedCount)
	}
	if _, err := db.Exec(`
		UPDATE articles
		SET summary_brief='shared brief',summary_detailed='shared detailed',word_count=999,reading_minutes=99
		WHERE feed_id=$1 AND url='https://shared-floor.example/recent'`, sharedFeedID); err != nil {
		t.Fatal(err)
	}
	refetchedAt := now.Add(time.Minute)
	if _, err := db.Exec(`
		UPDATE explore_articles SET fetched_at=$2
		WHERE source_id=$1 AND url='https://shared-floor.example/recent'`, sourceID, refetchedAt); err != nil {
		t.Fatal(err)
	}
	preserved, err := NewSubscribeService(db, func() time.Time { return refetchedAt }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.CopiedArticles != 1 {
		t.Fatalf("shared fetched-only result=%+v", preserved)
	}
	var preservedBrief, preservedDetailed sql.NullString
	var preservedWordCount, preservedReadingMinutes int
	if err := db.QueryRow(`
		SELECT summary_brief,summary_detailed,word_count,reading_minutes
		FROM articles
		WHERE feed_id=$1 AND url='https://shared-floor.example/recent'`, sharedFeedID).Scan(
		&preservedBrief, &preservedDetailed, &preservedWordCount, &preservedReadingMinutes,
	); err != nil {
		t.Fatal(err)
	}
	if !preservedBrief.Valid || preservedBrief.String != "shared brief" ||
		!preservedDetailed.Valid || preservedDetailed.String != "shared detailed" {
		t.Fatalf("shared fetched-only refresh cleared summaries brief=%#v detailed=%#v", preservedBrief, preservedDetailed)
	}
	if preservedWordCount != 2 || preservedReadingMinutes != 1 {
		t.Fatalf("shared preserved-content metrics=%d/%d want=2/1", preservedWordCount, preservedReadingMinutes)
	}

	contentFetchedAt := refetchedAt.Add(time.Minute)
	if _, err := db.Exec(`
		UPDATE explore_articles SET content='updated shared body',fetched_at=$2
		WHERE source_id=$1 AND url='https://shared-floor.example/recent'`, sourceID, contentFetchedAt); err != nil {
		t.Fatal(err)
	}
	changed, err := NewSubscribeService(db, func() time.Time { return contentFetchedAt }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CopiedArticles != 1 {
		t.Fatalf("shared content-change result=%+v", changed)
	}
	var changedBrief, changedDetailed sql.NullString
	var changedWordCount, changedReadingMinutes int
	if err := db.QueryRow(`
		SELECT summary_brief,summary_detailed,word_count,reading_minutes
		FROM articles
		WHERE feed_id=$1 AND url='https://shared-floor.example/recent'`, sharedFeedID).Scan(
		&changedBrief, &changedDetailed, &changedWordCount, &changedReadingMinutes,
	); err != nil {
		t.Fatal(err)
	}
	if changedBrief.Valid || changedDetailed.Valid {
		t.Fatalf("shared content refresh retained summaries brief=%#v detailed=%#v", changedBrief, changedDetailed)
	}
	if changedWordCount != 3 || changedReadingMinutes != 1 {
		t.Fatalf("shared changed-content metrics=%d/%d want=3/1", changedWordCount, changedReadingMinutes)
	}

	ownedSourceID := seedSubscribeSource(t, db, userID, "https://owned-floor.example/feed", "Owned", "valid", now)
	if _, err := db.Exec(`
		INSERT INTO explore_articles (source_id,url,normalized_url,title,content,published_at,fetched_at)
		VALUES
			($1,'https://owned-floor.example/old','https://owned-floor.example/old','Old','old body',$2,$3),
			($1,'https://owned-floor.example/undated','https://owned-floor.example/undated','Undated','undated body',NULL,$3)`,
		ownedSourceID, floor.Add(-time.Second), now); err != nil {
		t.Fatal(err)
	}
	owned, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, ownedSourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !owned.Created || owned.CopiedArticles != 2 {
		t.Fatalf("owned result=%+v", owned)
	}
}

func TestSubscribeRefreshClearsStaleSummariesAndRecomputesMetrics(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	userID := seedSubscribeUser(t, db, "subscribe-refresh-derived")
	sourceID := seedSubscribeSource(t, db, userID, "https://refresh-derived.example/feed", "Refresh", "valid", now)
	published := now.Add(-time.Hour)
	if _, err := db.Exec(`
		INSERT INTO explore_articles (source_id,url,normalized_url,title,content,published_at,fetched_at)
		VALUES ($1,'https://refresh-derived.example/post','https://refresh-derived.example/post','Old title','old body',$2,$3)`,
		sourceID, published, now); err != nil {
		t.Fatal(err)
	}
	first, err := NewSubscribeService(db, func() time.Time { return now }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE articles SET summary_brief='stale brief',summary_detailed='stale detailed',word_count=999,reading_minutes=99
		WHERE feed_id=$1`, first.FeedID); err != nil {
		t.Fatal(err)
	}
	newFetched := now.Add(time.Minute)
	if _, err := db.Exec(`
		UPDATE explore_articles
		SET fetched_at=$2
		WHERE source_id=$1`, sourceID, newFetched); err != nil {
		t.Fatal(err)
	}

	again, err := NewSubscribeService(db, func() time.Time { return newFetched }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if again.FeedID != first.FeedID || again.Created || again.CopiedArticles != 1 {
		t.Fatalf("again=%+v first=%+v", again, first)
	}
	var preservedBrief, preservedDetailed sql.NullString
	var preservedWordCount, preservedReadingMinutes int
	if err := db.QueryRow(`
		SELECT summary_brief,summary_detailed,word_count,reading_minutes
		FROM articles WHERE feed_id=$1`, first.FeedID).Scan(
		&preservedBrief, &preservedDetailed, &preservedWordCount, &preservedReadingMinutes,
	); err != nil {
		t.Fatal(err)
	}
	if !preservedBrief.Valid || preservedBrief.String != "stale brief" ||
		!preservedDetailed.Valid || preservedDetailed.String != "stale detailed" {
		t.Fatalf("fetched_at-only refresh cleared summaries brief=%#v detailed=%#v", preservedBrief, preservedDetailed)
	}
	if preservedWordCount != 2 || preservedReadingMinutes != 1 {
		t.Fatalf("preserved-content metrics=%d/%d want=2/1", preservedWordCount, preservedReadingMinutes)
	}

	newPublished := published.Add(time.Minute)
	contentFetched := newFetched.Add(time.Minute)
	if _, err := db.Exec(`
		UPDATE explore_articles
		SET title='New title',content='<p>Hello <strong>world</strong></p>',published_at=$2,fetched_at=$3
		WHERE source_id=$1`, sourceID, newPublished, contentFetched); err != nil {
		t.Fatal(err)
	}
	changed, err := NewSubscribeService(db, func() time.Time { return contentFetched }).SubscribeOne(userID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FeedID != first.FeedID || changed.Created || changed.CopiedArticles != 1 {
		t.Fatalf("changed=%+v first=%+v", changed, first)
	}

	var title, content sql.NullString
	var gotPublished, gotFetched sql.NullTime
	var brief, detailed sql.NullString
	var wordCount, readingMinutes int
	if err := db.QueryRow(`
		SELECT title,content,published_at,fetched_at,summary_brief,summary_detailed,word_count,reading_minutes
		FROM articles WHERE feed_id=$1`, first.FeedID).Scan(
		&title, &content, &gotPublished, &gotFetched, &brief, &detailed, &wordCount, &readingMinutes,
	); err != nil {
		t.Fatal(err)
	}
	if title.String != "New title" || content.String != "<p>Hello <strong>world</strong></p>" ||
		!gotPublished.Valid || !gotPublished.Time.Equal(newPublished) || !gotFetched.Valid || !gotFetched.Time.Equal(contentFetched) {
		t.Fatalf("refreshed title=%q content=%q published=%v fetched=%v", title.String, content.String, gotPublished, gotFetched)
	}
	if brief.Valid || detailed.Valid {
		t.Fatalf("stale summaries retained brief=%#v detailed=%#v", brief, detailed)
	}
	if wordCount != 2 || readingMinutes != 1 {
		t.Fatalf("metrics=%d/%d want=2/1", wordCount, readingMinutes)
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
