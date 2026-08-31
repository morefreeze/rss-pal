package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreArticleListItemBoundsUnicodeExcerptAndOmitsContent(t *testing.T) {
	prefix := strings.Repeat("界", 500)
	item := normalizeExploreArticleListItem(ExploreArticleListItem{
		ID:      1,
		Excerpt: prefix + "尾巴",
	})

	if !strings.HasPrefix(item.Excerpt, prefix) {
		t.Fatalf("excerpt lost its Unicode prefix: %q", item.Excerpt)
	}
	if got := len([]rune(item.Excerpt)); got != 500 {
		t.Fatalf("excerpt runes=%d want=500", got)
	}
	if !json.Valid([]byte(`{"excerpt":"` + item.Excerpt + `"}`)) {
		t.Fatalf("excerpt is not valid JSON-safe UTF-8: %q", item.Excerpt)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"content"`) {
		t.Fatalf("list DTO leaked content: %s", encoded)
	}
}

func TestBuildExplorePageQueryLimitsEachSourceToRecentFiveBeforeRequestedOrdering(t *testing.T) {
	query := strings.Join(strings.Fields(buildExplorePageQuery(ExploreListParams{
		Sort: SortCaptured,
		Dir:  SortAsc,
	})), " ")

	innerRecentFive := "ORDER BY COALESCE(explore_articles.published_at, explore_articles.fetched_at) DESC, explore_articles.fetched_at DESC, explore_articles.id DESC LIMIT 5"
	if !strings.Contains(query, innerRecentFive) {
		t.Fatalf("inner candidate order = %q, want fixed recent-five order %q", query, innerRecentFive)
	}
	outerRequestedOrder := ArticleOrderClause(ArticleAliasExplore, SortCaptured, SortAsc) + ", explore_articles.id ASC"
	if !strings.HasSuffix(query, outerRequestedOrder) {
		t.Fatalf("outer order = %q, want suffix %q", query, outerRequestedOrder)
	}
}

func TestExploreStableSourceDiversityPreservesSourceOrder(t *testing.T) {
	in := []ExploreArticleListItem{
		{ID: 11, SourceID: 1}, {ID: 12, SourceID: 1}, {ID: 13, SourceID: 1},
		{ID: 21, SourceID: 2}, {ID: 22, SourceID: 2}, {ID: 31, SourceID: 3},
	}
	got := stableDiversifyExploreArticles(in)
	want := []int{11, 12, 21, 13, 22, 31}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("ids[%d]=%d want=%d; got=%v", i, got[i].ID, want[i], got)
		}
	}
}

func TestExploreStableSourceDiversityIsStableWhenNoAlternativeExists(t *testing.T) {
	in := []ExploreArticleListItem{{ID: 1, SourceID: 7}, {ID: 2, SourceID: 7}, {ID: 3, SourceID: 7}}
	got := stableDiversifyExploreArticles(in)
	for i := range in {
		if got[i].ID != in[i].ID {
			t.Fatalf("got=%v want unchanged=%v", got, in)
		}
	}
}

func TestExploreRepositoryPageFeedbackVisibilityAndPagination(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID, otherUserID := insertExploreUsers(t, db)
	sourceA := insertExploreSource(t, db, "https://a.example/feed", "A")
	sourceB := insertExploreSource(t, db, "https://b.example/feed", "B")
	now := time.Now().UTC().Truncate(time.Second)
	batchID := insertExploreDoneBatch(t, db, userID, now, []exploreTestBatchSource{
		{sourceID: sourceA, rank: 1, topic: "programming"},
		{sourceID: sourceB, rank: 2, topic: "technology"},
	})
	_ = batchID
	insertExploreDoneBatch(t, db, otherUserID, now, []exploreTestBatchSource{{sourceID: sourceB, rank: 1, topic: "technology"}})
	for i := 0; i < 3; i++ {
		insertExploreArticle(t, db, sourceA, now.Add(-time.Duration(i)*time.Minute), "a")
	}
	for i := 0; i < 2; i++ {
		insertExploreArticle(t, db, sourceB, now.Add(-time.Duration(i+3)*time.Minute), "b")
	}

	repo := NewExploreRepository(db)
	page, err := repo.GetPage(userID, ExploreListParams{Limit: 3, Sort: SortCaptured, Dir: SortDesc})
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if !page.HasMore || len(page.Articles) != 3 {
		t.Fatalf("page=%+v want 3 articles and has_more", page)
	}
	if got := []int{page.Articles[0].SourceID, page.Articles[1].SourceID, page.Articles[2].SourceID}; got[0] != sourceA || got[1] != sourceA || got[2] != sourceB {
		t.Fatalf("diverse sources=%v", got)
	}
	if page.Snapshot.ID != batchID || page.Snapshot.Generating || page.Snapshot.UsingFallback {
		t.Fatalf("snapshot=%+v", page.Snapshot)
	}
	topicPage, err := repo.GetPage(userID, ExploreListParams{Limit: 20, Topic: "technology", Sort: SortCaptured, Dir: SortDesc})
	if err != nil || len(topicPage.Articles) != 2 {
		t.Fatalf("topic page=(%+v,%v), want two technology articles", topicPage, err)
	}
	for _, article := range topicPage.Articles {
		if article.SourceID != sourceB || article.Topic != "technology" {
			t.Fatalf("topic filter leaked article: %+v", article)
		}
	}
	sources, err := repo.GetSources(userID)
	if err != nil || len(sources) != 2 || sources[0].Rank != 1 || sources[0].RecentArticleCount != 3 {
		t.Fatalf("sources=(%+v,%v)", sources, err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET is_broken=true WHERE id=$1`, sourceA); err != nil {
		t.Fatalf("mark broken: %v", err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET validation_status='invalid',merged_into_source_id=$2 WHERE id=$1`, sourceB, sourceA); err != nil {
		t.Fatalf("mark merged: %v", err)
	}
	sources, err = repo.GetSources(userID)
	if err != nil || !sources[0].IsBroken || sources[1].MergedIntoSourceID == nil || *sources[1].MergedIntoSourceID != sourceA {
		t.Fatalf("source catalog states=(%+v,%v)", sources, err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET is_broken=false WHERE id=$1`, sourceA); err != nil {
		t.Fatalf("restore broken state: %v", err)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET validation_status='valid',merged_into_source_id=NULL WHERE id=$1`, sourceB); err != nil {
		t.Fatalf("restore merged state: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO explore_batches(user_id,slot_at,status) VALUES ($1,$2,'pending')`, userID, now.Add(3*time.Hour)); err != nil {
		t.Fatalf("insert pending batch: %v", err)
	}
	page, err = repo.GetPage(userID, ExploreListParams{Limit: 20})
	if err != nil || !page.Snapshot.Generating || page.Snapshot.UsingFallback {
		t.Fatalf("generating snapshot=(%+v,%v)", page.Snapshot, err)
	}
	if _, err := db.Exec(`UPDATE explore_batches SET status='failed',completed_at=$2 WHERE user_id=$1 AND slot_at=$2`, userID, now.Add(3*time.Hour)); err != nil {
		t.Fatalf("fail pending batch: %v", err)
	}
	page, err = repo.GetPage(userID, ExploreListParams{Limit: 20})
	if err != nil || page.Snapshot.Generating || !page.Snapshot.UsingFallback || !page.Snapshot.RefreshFailed {
		t.Fatalf("fallback snapshot=(%+v,%v)", page.Snapshot, err)
	}

	feedback, err := repo.CreateFeedback(userID, ExploreFeedbackInput{FeedbackType: model.ExploreFeedbackHideSource, SourceID: &sourceA})
	if err != nil {
		t.Fatalf("CreateFeedback: %v", err)
	}
	again, err := repo.CreateFeedback(userID, ExploreFeedbackInput{FeedbackType: model.ExploreFeedbackHideSource, SourceID: &sourceA})
	if err != nil || again.ID != feedback.ID {
		t.Fatalf("duplicate feedback=(%+v,%v), want same id=%d", again, err, feedback.ID)
	}
	page, err = repo.GetPage(userID, ExploreListParams{Limit: 20, Sort: SortCaptured, Dir: SortDesc})
	if err != nil {
		t.Fatalf("GetPage after feedback: %v", err)
	}
	for _, item := range page.Articles {
		if item.SourceID == sourceA {
			t.Fatalf("hidden source remained in page: %+v", item)
		}
	}
	if err := repo.DeleteFeedback(otherUserID, feedback.ID); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("cross-user delete=%v want ErrExploreNotFound", err)
	}
	if err := repo.DeleteFeedback(userID, feedback.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	privateSource := insertExploreSource(t, db, "https://private.example/feed", "private")
	insertExploreDoneBatch(t, db, otherUserID, now.Add(time.Hour), []exploreTestBatchSource{{sourceID: privateSource, rank: 1, topic: "security"}})
	if _, err := repo.CreateFeedback(userID, ExploreFeedbackInput{FeedbackType: model.ExploreFeedbackHideSource, SourceID: &privateSource}); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("unauthorized source feedback=%v want not found", err)
	}
}

func TestExploreRepositoryAscendingPageStillUsesEachSourcesRecentFive(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID, _ := insertExploreUsers(t, db)
	sourceID := insertExploreSource(t, db, "https://recent-five.example/feed", "recent-five")
	now := time.Now().UTC().Truncate(time.Second)
	insertExploreDoneBatch(t, db, userID, now, []exploreTestBatchSource{{sourceID: sourceID, rank: 1, topic: "programming"}})

	articleIDs := make([]int, 7)
	for i := range articleIDs {
		articleIDs[i] = insertExploreArticle(t, db, sourceID, now.Add(-time.Duration(i)*time.Hour), "recent-five")
	}

	page, err := NewExploreRepository(db).GetPage(userID, ExploreListParams{
		Limit: 20,
		Sort:  SortCaptured,
		Dir:   SortAsc,
	})
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	want := []int{articleIDs[4], articleIDs[3], articleIDs[2], articleIDs[1], articleIDs[0]}
	if len(page.Articles) != len(want) {
		t.Fatalf("article count=%d want=%d: %+v", len(page.Articles), len(want), page.Articles)
	}
	for i := range want {
		if page.Articles[i].ID != want[i] {
			t.Fatalf("article[%d].id=%d want=%d; page=%+v", i, page.Articles[i].ID, want[i], page.Articles)
		}
	}
}

func TestExploreRepositoryDetailVisibilityAndEventDenoising(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID, otherUserID := insertExploreUsers(t, db)
	sourceID := insertExploreSource(t, db, "https://detail.example/feed", "detail")
	now := time.Now().UTC().Truncate(time.Second)
	articleID := insertExploreArticle(t, db, sourceID, now, "detail")
	insertExploreDoneBatch(t, db, userID, now.Add(-29*24*time.Hour), []exploreTestBatchSource{{sourceID: sourceID, rank: 1, topic: "programming"}})

	repo := NewExploreRepository(db)
	detail, err := repo.GetVisibleArticle(userID, articleID)
	if err != nil || detail.ID != articleID || detail.Content == nil {
		t.Fatalf("visible detail=(%+v,%v)", detail, err)
	}
	if _, err := repo.GetVisibleArticle(otherUserID, articleID); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("cross-user detail=%v want not found", err)
	}
	created, err := repo.RecordArticleEvent(userID, articleID, model.ExploreArticleEventExposure, now)
	if err != nil || !created {
		t.Fatalf("first exposure=(%v,%v)", created, err)
	}
	created, err = repo.RecordArticleEvent(userID, articleID, model.ExploreArticleEventExposure, now.Add(time.Minute))
	if err != nil || created {
		t.Fatalf("duplicate exposure=(%v,%v), want de-noised", created, err)
	}
	created, err = repo.RecordArticleEvent(userID, articleID, model.ExploreArticleEventClick, now.Add(time.Minute))
	if err != nil || !created {
		t.Fatalf("click=(%v,%v)", created, err)
	}
	if _, err := repo.RecordArticleEvent(otherUserID, articleID, model.ExploreArticleEventClick, now); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("cross-user event=%v want not found", err)
	}

	if _, err := db.Exec(`UPDATE explore_batches SET completed_at=$2, slot_at=$2 WHERE user_id=$1`, userID, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("age batch: %v", err)
	}
	if _, err := repo.GetVisibleArticle(userID, articleID); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("stale detail=%v want not found", err)
	}
	if _, err := db.Exec(`INSERT INTO feeds (url,title,owner_id) VALUES ('https://detail.example/feed','formal',$1)`, userID); err != nil {
		t.Fatalf("insert formal feed: %v", err)
	}
	if _, err := repo.GetVisibleArticle(userID, articleID); err != nil {
		t.Fatalf("formal subscription should grant detail: %v", err)
	}
}

func TestExploreRepositoryInterestsReplaceAndTopicFeedbackFilter(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID, _ := insertExploreUsers(t, db)
	sourceID := insertExploreSource(t, db, "https://topics.example/feed", "topics")
	now := time.Now().UTC().Truncate(time.Second)
	insertExploreDoneBatch(t, db, userID, now, []exploreTestBatchSource{{sourceID: sourceID, rank: 1, topic: "distributed-systems"}})
	insertExploreArticle(t, db, sourceID, now, "topic")
	repo := NewExploreRepository(db)

	if _, err := repo.ReplaceInterests(userID, []string{"programming", "security", "programming"}); err != nil {
		t.Fatalf("ReplaceInterests: %v", err)
	}
	var interestCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM explore_feedback WHERE user_id=$1 AND feedback_type='boost_topic'`, userID).Scan(&interestCount); err != nil || interestCount != 2 {
		t.Fatalf("interest count=(%d,%v), want 2 unique", interestCount, err)
	}
	if _, err := repo.ReplaceInterests(userID, []string{"not-allowed"}); !errors.Is(err, ErrInvalidExploreInterest) {
		t.Fatalf("invalid interest=%v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM explore_feedback WHERE user_id=$1 AND feedback_type='boost_topic'`, userID).Scan(&interestCount); err != nil || interestCount != 2 {
		t.Fatalf("invalid replace changed prior interests: count=(%d,%v)", interestCount, err)
	}
	if _, err := repo.CreateFeedback(userID, ExploreFeedbackInput{FeedbackType: model.ExploreFeedbackDampenTopic, Topic: strPtr("distributed-systems")}); err != nil {
		t.Fatalf("dampen topic: %v", err)
	}
	if _, err := repo.CreateFeedback(userID, ExploreFeedbackInput{FeedbackType: model.ExploreFeedbackDampenTopic, Topic: strPtr("not-in-snapshot")}); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("unauthorized dampen topic=%v want not found", err)
	}
	page, err := repo.GetPage(userID, ExploreListParams{Limit: 20, Sort: SortPublished, Dir: SortDesc})
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if len(page.Articles) != 0 {
		t.Fatalf("dampened topic was not immediately filtered: %+v", page.Articles)
	}
}

func TestLockExploreInterestReplacementUsesStablePerUserAdvisoryKey(t *testing.T) {
	recorder := &exploreInterestLockRecorder{}
	if err := lockExploreInterestReplacement(recorder, 42); err != nil {
		t.Fatalf("lockExploreInterestReplacement: %v", err)
	}
	if got, want := strings.Join(strings.Fields(recorder.query), " "), "SELECT pg_advisory_xact_lock($1,$2)"; got != want {
		t.Fatalf("lock query=%q want=%q", got, want)
	}
	if len(recorder.args) != 2 || recorder.args[0] != exploreInterestReplacementAdvisoryNamespace || recorder.args[1] != 42 {
		t.Fatalf("lock args=%v want namespace=%d userID=42", recorder.args, exploreInterestReplacementAdvisoryNamespace)
	}
}

func TestExploreRepositoryConcurrentInterestReplacementSerializesPerUserOnly(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID, otherUserID := insertExploreUsers(t, db)

	firstTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer firstTx.Rollback()
	if _, err := NewExploreRepository(db).WithQuerier(firstTx).ReplaceInterests(userID, []string{"programming"}); err != nil {
		t.Fatalf("first ReplaceInterests: %v", err)
	}

	// The namespace/user pair must not serialize a different user's replacement.
	otherTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer otherTx.Rollback()
	otherDone := make(chan error, 1)
	go func() {
		_, replaceErr := NewExploreRepository(db).WithQuerier(otherTx).ReplaceInterests(otherUserID, []string{"security"})
		otherDone <- replaceErr
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("different-user replacement: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("different-user replacement was blocked by another user's lock")
	}
	if err := otherTx.Commit(); err != nil {
		t.Fatalf("commit different-user replacement: %v", err)
	}

	secondTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer secondTx.Rollback()
	if _, err := secondTx.Exec(`SET LOCAL lock_timeout = '3s'`); err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() {
		_, replaceErr := NewExploreRepository(db).WithQuerier(secondTx).ReplaceInterests(userID, []string{"health"})
		secondDone <- replaceErr
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("same-user replacement completed before the first transaction committed: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := firstTx.Commit(); err != nil {
		t.Fatalf("commit first replacement: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second ReplaceInterests: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("same-user replacement did not resume after the first commit")
	}
	if err := secondTx.Commit(); err != nil {
		t.Fatalf("commit second replacement: %v", err)
	}

	rows, err := db.Query(`SELECT topic FROM explore_feedback WHERE user_id=$1 AND feedback_type='boost_topic' ORDER BY topic`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var topics []string
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			t.Fatal(err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 || topics[0] != "health" {
		t.Fatalf("final interests=%v want last committed replacement [health]", topics)
	}
}

type exploreInterestLockRecorder struct {
	query string
	args  []interface{}
}

func (r *exploreInterestLockRecorder) Exec(query string, args ...interface{}) (sql.Result, error) {
	r.query = query
	r.args = append([]interface{}(nil), args...)
	return exploreNoopResult{}, nil
}

type exploreNoopResult struct{}

func (exploreNoopResult) LastInsertId() (int64, error) { return 0, nil }
func (exploreNoopResult) RowsAffected() (int64, error) { return 0, nil }

type exploreTestBatchSource struct {
	sourceID int
	rank     int
	topic    string
}

func insertExploreUsers(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	var a, b int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('explore-a','x') RETURNING id`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES ('explore-b','x') RETURNING id`).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func insertExploreSource(t *testing.T, db *sql.DB, url, title string) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,validation_status,is_broken,health_score) VALUES ($1,$2,$1,'valid',false,0.9) RETURNING id`, url, title).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertExploreDoneBatch(t *testing.T, db *sql.DB, userID int, at time.Time, sources []exploreTestBatchSource) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO explore_batches(user_id,slot_at,status,source_count,completed_at) VALUES ($1,$2,'done',$3,$2) RETURNING id`, userID, at, len(sources)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, err := db.Exec(`INSERT INTO explore_batch_sources(user_id,batch_id,source_id,rank,score,topic,reason) VALUES ($1,$2,$3,$4,1,$5,'because')`, userID, id, source.sourceID, source.rank, source.topic); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func insertExploreArticle(t *testing.T, db *sql.DB, sourceID int, at time.Time, slug string) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,excerpt,published_at,fetched_at) VALUES ($1,$2,$2,$3,'full content','excerpt',$4,$4) RETURNING id`, sourceID, "https://articles.example/"+slug+at.Format("150405.000000000"), slug, at).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func strPtr(value string) *string { return &value }
