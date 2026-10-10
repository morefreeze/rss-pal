package repository

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
	"time"
)

func TestExploreArticleStateIsolationUndoAndRetention(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	user, other := insertExploreUsers(t, db)
	source := insertExploreSource(t, db, "https://state.example/feed", "State")
	now := time.Now().UTC()
	for _, u := range []int{user, other} {
		insertExploreDoneBatch(t, db, u, now, []exploreTestBatchSource{{sourceID: source, rank: 1, topic: "programming"}})
	}
	article := insertExploreArticle(t, db, source, now, "state")
	sibling := insertExploreArticle(t, db, source, now.Add(-time.Minute), "sibling")
	repo := NewExploreRepository(db)
	yes, no := true, false
	state, err := repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Saved: &yes})
	if err != nil || !state.Saved || state.Skipped {
		t.Fatalf("save: %+v %v", state, err)
	}
	state, err = repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &yes})
	if err != nil || !state.Saved || !state.Skipped {
		t.Fatalf("partial skip: %+v %v", state, err)
	}
	page, err := repo.GetPage(user, ExploreListParams{})
	if err != nil || len(page.Articles) != 1 || page.Articles[0].ID != sibling {
		t.Fatalf("skip only current: %+v %v", page, err)
	}
	page, err = repo.GetPage(other, ExploreListParams{})
	if err != nil || len(page.Articles) != 2 || page.Articles[0].Saved {
		t.Fatalf("other account: %+v %v", page, err)
	}
	page, err = repo.GetPage(user, ExploreListParams{View: "later"})
	if err != nil || len(page.Articles) != 1 || page.Articles[0].ID != article || !page.Articles[0].Saved {
		t.Fatalf("saved+skipped: %+v %v", page, err)
	}
	if _, err = db.Exec(`UPDATE explore_batches SET completed_at=NOW()-INTERVAL '40 days'; UPDATE recommended_feeds SET validation_status='invalid'`); err != nil {
		t.Fatal(err)
	}
	if err = NewExploreCatalogRepository(db).RetainArticles(source, now.Add(60*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.GetVisibleArticle(user, article)
	if err != nil || !detail.Saved || detail.Content == nil {
		t.Fatalf("saved survives expiry/pruning: %+v %v", detail, err)
	}
	if _, err = repo.GetVisibleArticle(other, article); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("saved visibility leaked: %v", err)
	}
	if _, err = repo.UpdateArticleState(other, article, ExploreArticleStatePatch{Saved: &yes}); !errors.Is(err, ErrExploreNotFound) {
		t.Fatalf("unauthorized save: %v", err)
	}
	state, err = repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &no})
	if err != nil || state.Skipped || !state.Saved {
		t.Fatalf("undo: %+v %v", state, err)
	}
	if _, err = repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Saved: &no}); err != nil {
		t.Fatal(err)
	}
	page, err = repo.GetPage(user, ExploreListParams{View: "later"})
	if err != nil || len(page.Articles) != 0 {
		t.Fatalf("unsave: %+v %v", page, err)
	}
	var count int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM explore_feedback)+(SELECT count(*) FROM feeds)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("neutral action changed feedback/subscriptions: %d %v", count, err)
	}
}

func TestExploreArticleSkipSurvivesCacheReingestion(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	user, _ := insertExploreUsers(t, db)
	source := insertExploreSource(t, db, "https://skip.example/feed", "Skip")
	now := time.Now().UTC()
	insertExploreDoneBatch(t, db, user, now, []exploreTestBatchSource{{sourceID: source, rank: 1, topic: "programming"}})
	article := insertExploreArticle(t, db, source, now, "same")
	repo := NewExploreRepository(db)
	yes := true
	if _, err := repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM explore_articles WHERE id=$1`, article); err != nil {
		t.Fatal(err)
	}
	insertExploreArticle(t, db, source, now, "same")
	page, err := repo.GetPage(user, ExploreListParams{})
	if err != nil || len(page.Articles) != 0 {
		t.Fatalf("skip lost after reingestion: %+v %v", page, err)
	}
}

func TestExploreArticleStateRLS(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	user, other := insertExploreUsers(t, db)
	source := insertExploreSource(t, db, "https://rls.example/feed", "RLS")
	now := time.Now().UTC()
	article := insertExploreArticle(t, db, source, now, "rls")
	for _, u := range []int{user, other} {
		insertExploreDoneBatch(t, db, u, now, []exploreTestBatchSource{{sourceID: source, rank: 1}})
	}
	yes := true
	if _, err := NewExploreRepository(db).UpdateArticleState(other, article, ExploreArticleStatePatch{Saved: &yes}); err != nil {
		t.Fatal(err)
	}
	tx, err := app.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('app.user_id',$1,true)`, user); err != nil {
		t.Fatal(err)
	}
	repo := NewExploreRepository(app).WithQuerier(tx)
	if _, err = repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &yes}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.GetPage(other, ExploreListParams{View: "later"})
	if err != nil || len(page.Articles) != 0 {
		t.Fatalf("RLS leaked saved rows: %+v %v", page, err)
	}
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM explore_article_states`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("RLS: %d %v", count, err)
	}
}

func TestExploreSavedPaginationTopicAndHardCapRetention(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	user, _ := insertExploreUsers(t, db)
	source := insertExploreSource(t, db, "https://saved-pages.example/feed", "Pages")
	now := time.Now().UTC()
	insertExploreDoneBatch(t, db, user, now, []exploreTestBatchSource{{sourceID: source, rank: 1, topic: "programming"}})
	repo := NewExploreRepository(db)
	yes := true
	saved := []int{}
	for i := 0; i < 3; i++ {
		id := insertExploreArticle(t, db, source, now.Add(-time.Duration(i+100)*time.Minute), "saved")
		if _, err := repo.UpdateArticleState(user, id, ExploreArticleStatePatch{Saved: &yes}); err != nil {
			t.Fatal(err)
		}
		saved = append(saved, id)
	}
	for i := 0; i < 55; i++ {
		insertExploreArticle(t, db, source, now.Add(-time.Duration(i)*time.Minute), "new")
	}
	if err := NewExploreCatalogRepository(db).RetainArticles(source, now); err != nil {
		t.Fatal(err)
	}
	page, err := repo.GetPage(user, ExploreListParams{View: "later", Limit: 2, Topic: "programming"})
	if err != nil || len(page.Articles) != 2 || !page.HasMore || page.Articles[0].ID != saved[0] {
		t.Fatalf("saved page: %+v %v", page, err)
	}
	page, err = repo.GetPage(user, ExploreListParams{View: "later", Limit: 2, Offset: 2, Topic: "programming"})
	if err != nil || len(page.Articles) != 1 || page.HasMore || page.Articles[0].ID != saved[2] {
		t.Fatalf("saved page2: %+v %v", page, err)
	}
}

func TestExploreSkipColdRecommendations(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	user, _ := insertExploreUsers(t, db)
	source := insertExploreSource(t, db, "https://cold-state.example/feed", "Cold")
	now := time.Now().UTC()
	article := insertExploreArticle(t, db, source, now, "cold")
	var provider int
	if err := db.QueryRow(`INSERT INTO explore_registry_providers(provider_key,provider_kind,endpoint) VALUES ('state-cold','directory','https://directory.example') RETURNING id`).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations(provider_id,source_id,external_key,last_seen_at) VALUES ($1,$2,'cold',NOW())`, provider, source); err != nil {
		t.Fatal(err)
	}
	repo := NewExploreRepository(db)
	yes, no := true, false
	if _, err := repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &yes}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.GetPage(user, ExploreListParams{})
	if err != nil || len(page.Articles) != 0 {
		t.Fatalf("cold skip: %+v %v", page, err)
	}
	if _, err = repo.UpdateArticleState(user, article, ExploreArticleStatePatch{Skipped: &no}); err != nil {
		t.Fatal(err)
	}
	page, err = repo.GetPage(user, ExploreListParams{})
	if err != nil || len(page.Articles) != 1 {
		t.Fatalf("cold undo: %+v %v", page, err)
	}
}
