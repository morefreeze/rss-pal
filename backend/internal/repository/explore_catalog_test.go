package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreCatalogRetentionSQLKeepsHardCapAndValidSourceFloor(t *testing.T) {
	for _, fragment := range []string{
		"ROW_NUMBER() OVER",
		"ranked.position > 50",
		"ranked.effective_at < $2",
		"source.validation_status <> 'valid'",
		"ranked.position > 5",
	} {
		if !strings.Contains(exploreArticleRetentionSQL, fragment) {
			t.Errorf("retention SQL missing %q", fragment)
		}
	}
}

func TestExploreCatalogStateAndUpsertSQLPreserveRecoveryPath(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		query     string
		required  []string
		forbidden []string
	}{
		{
			name:  "transient failure remains valid",
			query: exploreFetchFailureSQL,
			required: []string{
				"health_score=GREATEST(0,COALESCE(health_score,1)-0.25)",
				"is_broken=(GREATEST(0,COALESCE(health_score,1)-0.25)=0)",
			},
			forbidden: []string{"validation_status="},
		},
		{
			name:     "success fully restores health",
			query:    exploreFetchSuccessSQL,
			required: []string{"validation_status='valid'", "health_score=1", "is_broken=false"},
		},
		{
			name:  "nullable article fields preserve last good values",
			query: exploreArticleUpsertSQL,
			required: []string{
				"content=COALESCE(EXCLUDED.content,explore_articles.content)",
				"excerpt=COALESCE(EXCLUDED.excerpt,explore_articles.excerpt)",
				"published_at=COALESCE(EXCLUDED.published_at,explore_articles.published_at)",
			},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, fragment := range fixture.required {
				if !strings.Contains(fixture.query, fragment) {
					t.Errorf("SQL missing %q", fragment)
				}
			}
			for _, fragment := range fixture.forbidden {
				if strings.Contains(fixture.query, fragment) {
					t.Errorf("SQL unexpectedly contains %q", fragment)
				}
			}
		})
	}
}

func TestExploreCatalogBulkWriteLocksCanonicalSourceBeforeRetention(t *testing.T) {
	for _, fragment := range []string{"recommended_feeds", "WHERE id=$1", "FOR UPDATE"} {
		if !strings.Contains(exploreSourceWriteLockSQL, fragment) {
			t.Errorf("source write lock SQL missing %q", fragment)
		}
	}
}

func TestExploreCatalogLoadsCanonicalSourceAndProviderObservations(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	sourceID := insertCatalogSource(t, db, "https://catalog.example/feed", model.ExploreValidationPending, nil, nil)

	providerIDs := make([]int, 2)
	for index, fixture := range []struct{ key, kind string }{{"catalog-opml", model.ExploreProviderKindOPML}, {"catalog-reddit", model.ExploreProviderKindRedditStream}} {
		if err := db.QueryRow(`
			INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, topic)
			VALUES ($1, $2, $3, 'go') RETURNING id`, fixture.key, fixture.kind, "https://registry.example/"+fixture.key).Scan(&providerIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	observedAt := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	for index, providerID := range providerIDs {
		if _, err := db.Exec(`
			INSERT INTO explore_source_observations
			(provider_id, source_id, external_key, provider_tags, first_seen_at, last_seen_at, occurrence_count)
			VALUES ($1, $2, $3, ARRAY['go'], $4, $4, $5)`, providerID, sourceID, fmt.Sprintf("external-%d", index), observedAt.Add(time.Duration(index)*time.Hour), index+1); err != nil {
			t.Fatal(err)
		}
	}

	catalog, err := NewExploreCatalogRepository(db).GetSourceWithObservations(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source.ID != sourceID || catalog.Source.NormalizedURL != "https://catalog.example/feed" {
		t.Fatalf("canonical source=%+v", catalog.Source)
	}
	if len(catalog.Observations) != 2 {
		t.Fatalf("observations=%+v", catalog.Observations)
	}
	if catalog.Observations[0].ProviderKind != model.ExploreProviderKindRedditStream || catalog.Observations[1].ProviderKind != model.ExploreProviderKindOPML {
		t.Fatalf("provider kinds/order=%+v", catalog.Observations)
	}
}

func TestExploreCatalogListsOnlyDueCanonicalSources(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	old := now.Add(-6 * time.Hour)
	fresh := now.Add(-10 * time.Minute)
	pendingID := insertCatalogSource(t, db, "https://pending.example/feed", model.ExploreValidationPending, &old, nil)
	invalidID := insertCatalogSource(t, db, "https://invalid.example/feed", model.ExploreValidationInvalid, &fresh, nil)
	oldInvalidID := insertCatalogSource(t, db, "https://old-invalid.example/feed", model.ExploreValidationInvalid, &old, nil)
	validID := insertCatalogSource(t, db, "https://valid.example/feed", model.ExploreValidationValid, &old, &old)
	_ = insertCatalogSource(t, db, "https://recent-failure.example/feed", model.ExploreValidationValid, &fresh, &old)
	_ = insertCatalogSource(t, db, "https://fresh.example/feed", model.ExploreValidationValid, &fresh, &fresh)

	due, err := NewExploreCatalogRepository(db).ListDueSources(now.Add(-time.Hour), now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 || due[0].ID != pendingID || due[1].ID != validID {
		t.Fatalf("due=%+v invalidIDs=%d,%d", due, invalidID, oldInvalidID)
	}
	limited, err := NewExploreCatalogRepository(db).ListDueSources(now.Add(-time.Hour), now.Add(-time.Hour), 1)
	if err != nil || len(limited) != 1 || limited[0].ID != pendingID {
		t.Fatalf("limited=%+v err=%v", limited, err)
	}
}

func TestExploreCatalogTransitionsHealthWithoutDeletingLastGoodCache(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://health.example/feed", model.ExploreValidationPending, nil, nil)
	repo := NewExploreCatalogRepository(db)
	if err := repo.MarkValidationPending(sourceID); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkValidationValid(sourceID, now, `"v1"`, "Sun, 31 Aug 2026 12:00:00 GMT"); err != nil {
		t.Fatal(err)
	}
	articleID, err := repo.UpsertArticle(model.ExploreArticle{
		SourceID: sourceID, URL: "https://health.example/article", NormalizedURL: "https://health.example/article",
		Title: "last good", FetchedAt: now, PublishedAt: ptrTime(now.Add(-time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 4; attempt++ {
		if err := repo.RecordFetchFailure(sourceID, now.Add(time.Duration(attempt)*time.Hour), errors.New("temporary failure")); err != nil {
			t.Fatal(err)
		}
		var currentScore float64
		if err := db.QueryRow(`SELECT health_score FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&currentScore); err != nil {
			t.Fatal(err)
		}
		wantScore := 1 - float64(attempt)*0.25
		if currentScore != wantScore {
			t.Fatalf("attempt=%d score=%v want=%v", attempt, currentScore, wantScore)
		}
	}
	var status string
	var score float64
	var isBroken bool
	var articleCount int
	if err := db.QueryRow(`SELECT validation_status, health_score, is_broken FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &score, &isBroken); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE id=$1 AND source_id=$2`, articleID, sourceID).Scan(&articleCount); err != nil {
		t.Fatal(err)
	}
	if status != model.ExploreValidationValid || score != 0 || !isBroken || articleCount != 1 {
		t.Fatalf("after failures status=%q score=%v broken=%t cached=%d", status, score, isBroken, articleCount)
	}
	notDue, err := repo.ListDueSources(now.Add(24*time.Hour), now.Add(3*time.Hour+59*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notDue) != 0 {
		t.Fatalf("failed source immediately due again: %+v", notDue)
	}
	due, err := repo.ListDueSources(now.Add(24*time.Hour), now.Add(4*time.Hour+time.Second), 10)
	if err != nil || len(due) != 1 || due[0].ID != sourceID {
		t.Fatalf("source not due after health interval: due=%+v err=%v", due, err)
	}

	recoveredAt := now.Add(5 * time.Hour)
	if err := repo.RecordFetchSuccess(sourceID, recoveredAt, `"v2"`, "Sun, 31 Aug 2026 17:00:00 GMT"); err != nil {
		t.Fatal(err)
	}
	var etag, modified string
	var fetchedAt time.Time
	if err := db.QueryRow(`SELECT validation_status, health_score, is_broken, etag, last_modified, last_fetched_at FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &score, &isBroken, &etag, &modified, &fetchedAt); err != nil {
		t.Fatal(err)
	}
	if status != model.ExploreValidationValid || score != 1 || isBroken || etag != `"v2"` || modified == "" || !fetchedAt.Equal(recoveredAt) {
		t.Fatalf("recovered status=%q score=%v broken=%t etag=%q modified=%q fetched=%v", status, score, isBroken, etag, modified, fetchedAt)
	}
}

func TestExploreCatalogMarkInvalidPreservesConditionalStateAndArticles(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://invalid-state.example/feed", model.ExploreValidationValid, &now, &now)
	if _, err := db.Exec(`UPDATE recommended_feeds SET etag='old-tag', last_modified='old-date', health_score=1 WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_articles (source_id,url,normalized_url,title,fetched_at) VALUES ($1,'https://invalid-state.example/a','https://invalid-state.example/a','old',$2)`, sourceID, now); err != nil {
		t.Fatal(err)
	}

	if err := NewExploreCatalogRepository(db).MarkValidationInvalid(sourceID, now.Add(time.Hour), errors.New("not enough recent articles")); err != nil {
		t.Fatal(err)
	}
	var status, etag, modified, lastError string
	var count int
	if err := db.QueryRow(`SELECT validation_status,etag,last_modified,last_error FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &etag, &modified, &lastError); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, sourceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if status != model.ExploreValidationInvalid || etag != "old-tag" || modified != "old-date" || lastError == "" || count != 1 {
		t.Fatalf("status=%q etag=%q modified=%q error=%q count=%d", status, etag, modified, lastError, count)
	}
}

func TestExploreCatalogArticleUpsertAndExactRetention(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repo := NewExploreCatalogRepository(db)
	validID := insertCatalogSource(t, db, "https://low-frequency.example/feed", model.ExploreValidationValid, &now, &now)
	invalidID := insertCatalogSource(t, db, "https://expired.example/feed", model.ExploreValidationInvalid, &now, nil)

	oldContent, oldExcerpt := "last good content", "last good excerpt"
	oldPublished := now.Add(-50 * 24 * time.Hour)
	first := model.ExploreArticle{SourceID: validID, URL: "https://low-frequency.example/alias", NormalizedURL: "https://low-frequency.example/a", Title: "first", Content: &oldContent, Excerpt: &oldExcerpt, FetchedAt: now, PublishedAt: &oldPublished}
	firstID, err := repo.UpsertArticle(first)
	if err != nil {
		t.Fatal(err)
	}
	first.URL, first.Title, first.Content, first.Excerpt, first.PublishedAt = "https://low-frequency.example/canonical", "updated", nil, nil, nil
	secondID, err := repo.UpsertArticle(first)
	if err != nil || secondID != firstID {
		t.Fatalf("upsert secondID=%d firstID=%d err=%v", secondID, firstID, err)
	}
	var gotTitle, gotURL string
	var gotContent, gotExcerpt sql.NullString
	var gotPublished time.Time
	if err := db.QueryRow(`SELECT title,url,content,excerpt,published_at FROM explore_articles WHERE id=$1`, firstID).Scan(&gotTitle, &gotURL, &gotContent, &gotExcerpt, &gotPublished); err != nil {
		t.Fatal(err)
	}
	if gotTitle != "updated" || gotURL != first.URL || gotContent.String != oldContent || gotExcerpt.String != oldExcerpt || !gotPublished.Equal(oldPublished) {
		t.Fatalf("upsert title=%q url=%q content=%v excerpt=%v published=%v", gotTitle, gotURL, gotContent, gotExcerpt, gotPublished)
	}
	empty := ""
	first.Content, first.Excerpt = &empty, &empty
	if _, err := repo.UpsertArticle(first); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT content,excerpt FROM explore_articles WHERE id=$1`, firstID).Scan(&gotContent, &gotExcerpt); err != nil {
		t.Fatal(err)
	}
	if !gotContent.Valid || gotContent.String != "" || !gotExcerpt.Valid || gotExcerpt.String != "" {
		t.Fatalf("explicit empty strings were not persisted: content=%v excerpt=%v", gotContent, gotExcerpt)
	}

	insertCatalogArticles(t, db, validID, now.Add(-41*24*time.Hour), 7)
	insertCatalogArticles(t, db, invalidID, now.Add(-40*24*time.Hour), 8)
	if err := repo.RetainArticles(validID, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.RetainArticles(invalidID, now); err != nil {
		t.Fatal(err)
	}
	var validCount, invalidCount int
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, validID).Scan(&validCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, invalidID).Scan(&invalidCount); err != nil {
		t.Fatal(err)
	}
	if validCount != 5 || invalidCount != 0 {
		t.Fatalf("old retention valid=%d invalid=%d", validCount, invalidCount)
	}

	capID := insertCatalogSource(t, db, "https://cap.example/feed", model.ExploreValidationValid, &now, &now)
	insertCatalogArticles(t, db, capID, now, 55)
	if err := repo.RetainArticles(capID, now); err != nil {
		t.Fatal(err)
	}
	var capCount int
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, capID).Scan(&capCount); err != nil {
		t.Fatal(err)
	}
	if capCount != 50 {
		t.Fatalf("hard cap count=%d", capCount)
	}
	if err := db.QueryRow(`SELECT title,url,content,excerpt FROM explore_articles WHERE id=$1`, firstID).Scan(&gotTitle, &gotURL, &gotContent, &gotExcerpt); err != sql.ErrNoRows {
		t.Fatalf("old upserted article should be trimmed after retention, got title=%q url=%q content=%v excerpt=%v err=%v", gotTitle, gotURL, gotContent, gotExcerpt, err)
	}
}

func insertCatalogSource(t *testing.T, db *sql.DB, feedURL, status string, checkedAt, fetchedAt *time.Time) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO recommended_feeds
		(url,title,category,language,normalized_url,validation_status,last_checked_at,last_fetched_at,health_score)
		VALUES ($1,$1,'test','en',$1,$2,$3,$4,CASE WHEN $2='valid' THEN 1 ELSE NULL END)
		RETURNING id`, feedURL, status, checkedAt, fetchedAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCatalogArticles(t *testing.T, db *sql.DB, sourceID int, newest time.Time, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		publishedAt := newest.Add(-time.Duration(index) * time.Hour)
		articleURL := fmt.Sprintf("https://articles.example/%d/%d", sourceID, index)
		if _, err := db.Exec(`INSERT INTO explore_articles (source_id,url,normalized_url,title,published_at,fetched_at) VALUES ($1,$2,$2,$3,$4,$4)`, sourceID, articleURL, fmt.Sprintf("article-%d", index), publishedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
