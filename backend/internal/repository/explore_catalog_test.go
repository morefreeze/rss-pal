package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/lib/pq"
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
				"COALESCE(EXCLUDED.content,explore_articles.content)",
				"COALESCE(EXCLUDED.excerpt,explore_articles.excerpt)",
				"COALESCE(EXCLUDED.published_at,explore_articles.published_at)",
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

func TestExploreCatalogDueSQLRequiresEnabledObservation(t *testing.T) {
	for _, fragment := range []string{
		"EXISTS (",
		"explore_source_observations observation",
		"explore_registry_providers provider",
		"observation.source_id = source.id",
		"provider.enabled",
		"source.is_broken=false",
		"source.is_broken=true",
		"observation.last_seen_at > COALESCE(source.last_checked_at,source.last_fetched_at)",
		"COALESCE(source.last_checked_at,source.last_fetched_at) <= $3",
		"WHEN source.is_broken=false THEN 1",
		"ELSE 3",
		"LIMIT $4",
	} {
		if !strings.Contains(exploreDueSourcesSQL, fragment) {
			t.Errorf("due-source SQL missing %q", fragment)
		}
	}
	normalIndex := strings.Index(exploreDueSourcesSQL, "WHEN source.is_broken=false THEN 1")
	freshBrokenIndex := strings.Index(exploreDueSourcesSQL, "WHEN source.is_broken AND EXISTS")
	if normalIndex < 0 || freshBrokenIndex < 0 || normalIndex > freshBrokenIndex {
		t.Fatalf("normal refreshes must sort before every broken health check: %s", exploreDueSourcesSQL)
	}
}

func TestExploreCatalogBrokenHealthChecksCannotDisplaceNormalRefreshFromLimit(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	providerID := insertCatalogProvider(t, db, "broken-budget-enabled", true)
	if _, err := db.Exec(`
		INSERT INTO recommended_feeds
		(url,title,category,language,normalized_url,validation_status,last_checked_at,last_fetched_at,health_score,is_broken)
		SELECT 'https://broken-budget-' || value || '.example/feed',
		       'broken budget','test','en','https://broken-budget-' || value || '.example/feed',
		       'valid',$1,$1,0,true
		FROM generate_series(1,500) value`, now.Add(-25*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var normalID int
	if err := db.QueryRow(`
		INSERT INTO recommended_feeds
		(url,title,category,language,normalized_url,validation_status,last_checked_at,last_fetched_at,health_score,is_broken)
		VALUES ('https://normal-budget.example/feed','normal budget','test','en','https://normal-budget.example/feed','valid',$1,$1,1,false)
		RETURNING id`, now.Add(-4*time.Hour)).Scan(&normalID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO explore_source_observations(provider_id,source_id,external_key,first_seen_at,last_seen_at)
		SELECT $1,id,'budget-' || id,$2,$2 FROM recommended_feeds`, providerID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	due, err := NewExploreCatalogRepository(db).ListDueSources(now.Add(-30*time.Minute), now.Add(-3*time.Hour), now.Add(-24*time.Hour), 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 500 {
		t.Fatalf("due count=%d want=500", len(due))
	}
	for _, source := range due {
		if source.ID == normalID {
			return
		}
	}
	t.Fatalf("normal refresh %d was displaced by 500 broken health checks", normalID)
}

func TestExploreCatalogConditionalRequestSQLSeparates200And304(t *testing.T) {
	for name, query := range map[string]string{
		"validation 200": exploreValidationValidSQL,
		"refresh 200":    exploreFetchSuccessSQL,
	} {
		t.Run(name, func(t *testing.T) {
			for _, fragment := range []string{"etag=NULLIF($3,'')", "last_modified=NULLIF($4,'')"} {
				if !strings.Contains(query, fragment) {
					t.Errorf("200 SQL missing %q", fragment)
				}
			}
		})
	}
	for _, fragment := range []string{"last_checked_at=$2", "last_fetched_at=$2", "health_score=1"} {
		if !strings.Contains(exploreFetchNotModifiedSQL, fragment) {
			t.Errorf("304 SQL missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"etag=", "last_modified="} {
		if strings.Contains(exploreFetchNotModifiedSQL, forbidden) {
			t.Errorf("304 SQL unexpectedly mutates validator with %q", forbidden)
		}
	}
}

func TestExploreCatalogArticleUpsertIsFetchedAtMonotonic(t *testing.T) {
	if count := strings.Count(exploreArticleUpsertSQL, "EXCLUDED.fetched_at >= explore_articles.fetched_at"); count < 7 {
		t.Fatalf("article upsert only guards %d replacement fields by fetched_at", count)
	}
	if strings.Contains(exploreArticleUpsertSQL, "DO UPDATE SET\n") && strings.Contains(exploreArticleUpsertSQL, "WHERE explore_articles.fetched_at") {
		t.Fatal("article upsert WHERE can suppress RETURNING existing id")
	}
}

func TestExploreCatalogMutationSQLUsesMonotonicCheckedAtFence(t *testing.T) {
	for name, query := range map[string]string{
		"validation valid":   exploreValidationValidSQL,
		"validation invalid": exploreValidationInvalidSQL,
		"fetch failure":      exploreFetchFailureSQL,
		"fetch success":      exploreFetchSuccessSQL,
		"fetch not modified": exploreFetchNotModifiedSQL,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(query, "last_checked_at IS NULL OR last_checked_at <= $2") {
				t.Fatalf("mutation SQL lacks monotonic checked-at fence: %s", query)
			}
		})
	}
}

func TestExploreCatalogAdoptionSQLPreservesCanonicalMergeInvariants(t *testing.T) {
	for _, fragment := range []string{"pg_advisory_xact_lock", "FOR UPDATE"} {
		if !strings.Contains(exploreCanonicalAdvisoryLockSQL+exploreAdoptSourceLockSQL, fragment) {
			t.Fatalf("adoption locking SQL missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"ON CONFLICT (provider_id, external_key, source_id)",
		"unnest",
		"ORDER BY tag",
		"LEAST(",
		"GREATEST(",
		"occurrence_count",
	} {
		if !strings.Contains(exploreMergeObservationsSQL, fragment) {
			t.Fatalf("observation merge SQL missing %q", fragment)
		}
	}
	if strings.Contains(exploreMergeObservationsSQL, "occurrence_count+") || strings.Contains(exploreMergeObservationsSQL, "occurrence_count +") {
		t.Fatal("observation merge adds duplicate evidence counts")
	}
	if !strings.Contains(exploreAdoptPairLockSQL, "ORDER BY id FOR UPDATE") {
		t.Fatal("adoption pair lock is not deterministic")
	}
	if !strings.Contains(exploreRegistryCandidateUpsertSQL, "ON CONFLICT (normalized_url)") {
		t.Fatal("registry source upsert no longer shares canonical identity")
	}
}

func TestExploreAdoptionDecisionUsesExplicitMergePointers(t *testing.T) {
	const sourceID, targetID, otherID = 1, 2, 3
	ptr := func(id int) *int { return &id }
	for _, tc := range []struct {
		name           string
		source, target exploreAdoptionSourceState
		want           exploreAdoptionDecision
	}{
		{"pending into valid", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationPending}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptMergeIntoTarget},
		{"invalid into valid", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptMergeIntoTarget},
		{"valid source adopts ordinary invalid target", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid}, exploreAdoptMergeIntoTarget},
		{"pending source adopts ordinary invalid target", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationPending}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid}, exploreAdoptMergeIntoTarget},
		{"source already merged into target", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(targetID)}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptReturnTarget},
		{"target was merged into source", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(sourceID)}, exploreAdoptPreserveSource},
		{"both invalid without explicit relation fail closed", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid}, exploreAdoptFailClosed},
		{"source points elsewhere", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(otherID)}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptFailClosed},
		{"target points elsewhere", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationValid}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(otherID)}, exploreAdoptFailClosed},
		{"cycle fails closed", exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(targetID)}, exploreAdoptionSourceState{ValidationStatus: model.ExploreValidationInvalid, MergedIntoSourceID: ptr(sourceID)}, exploreAdoptFailClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideExploreAdoption(sourceID, targetID, tc.source, tc.target); got != tc.want {
				t.Fatalf("decision=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestExploreSameURLAdoptionDecisionRejectsNonRootTargets(t *testing.T) {
	const sourceID, targetID, otherID = 1, 2, 3
	ptr := func(id int) *int { return &id }
	for _, tc := range []struct {
		name           string
		source, target exploreAdoptionSourceState
		want           exploreAdoptionDecision
	}{
		{"ordinary source stays canonical", exploreAdoptionSourceState{}, exploreAdoptionSourceState{}, exploreAdoptPreserveSource},
		{"direct tombstone returns root", exploreAdoptionSourceState{MergedIntoSourceID: ptr(targetID)}, exploreAdoptionSourceState{}, exploreAdoptReturnTarget},
		{"chain fails closed", exploreAdoptionSourceState{MergedIntoSourceID: ptr(targetID)}, exploreAdoptionSourceState{MergedIntoSourceID: ptr(otherID)}, exploreAdoptFailClosed},
		{"cycle fails closed", exploreAdoptionSourceState{MergedIntoSourceID: ptr(targetID)}, exploreAdoptionSourceState{MergedIntoSourceID: ptr(sourceID)}, exploreAdoptFailClosed},
		{"self loop fails closed", exploreAdoptionSourceState{MergedIntoSourceID: ptr(sourceID)}, exploreAdoptionSourceState{}, exploreAdoptFailClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideExploreSameURLAdoption(sourceID, targetID, tc.source, tc.target); got != tc.want {
				t.Fatalf("decision=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestValidateExploreCanonicalFeedURL(t *testing.T) {
	valid := []string{
		"https://example.com/feed",
		"http://news.example:8080/rss?format=xml",
		"https://[2001:4860:4860::8888]/feed",
	}
	for _, raw := range valid {
		if err := validateExploreCanonicalFeedURL(raw); err != nil {
			t.Errorf("valid URL %q: %v", raw, err)
		}
	}
	invalid := []string{
		"", "ftp://example.com/feed", "https://user:pass@example.com/feed",
		"https://example.com/feed#fragment", "https://EXAMPLE.com/feed",
		"https://example.com/feed?utm_source=x", "https://localhost/feed",
		"https://api.localhost/feed", "http://127.0.0.1/feed", "http://10.0.0.1/feed",
		"http://[::]/feed", "http://[ff02::1]/feed", "https://example.com/\x00feed",
		"https://example.com/" + strings.Repeat("x", 2049),
	}
	for _, raw := range invalid {
		if err := validateExploreCanonicalFeedURL(raw); err == nil {
			t.Errorf("invalid URL accepted: %q", raw)
		}
	}
}

func TestExploreCatalogUpsertArticlesRejectsMoreThan50BeforeTransaction(t *testing.T) {
	repo := NewExploreCatalogRepository(nil)
	tooMany := make([]model.ExploreArticle, 51)
	if err := repo.UpsertArticles(1, tooMany, time.Now()); !errors.Is(err, ErrExploreArticleBatchTooLarge) {
		t.Fatalf("51 articles err=%v", err)
	}
	atLimit := make([]model.ExploreArticle, 50)
	if err := repo.UpsertArticles(1, atLimit, time.Now()); err == nil || err.Error() != "txOrBegin: Querier is neither *sql.Tx nor *sql.DB" {
		t.Fatalf("50 articles did not pass size gate into transaction setup: %v", err)
	}
}

func TestExploreCatalogSourceNotFoundErrorIsConsistent(t *testing.T) {
	if got := exploreSourceNotFoundError(42).Error(); got != "explore source 42 not found" {
		t.Fatalf("not-found error=%q", got)
	}
}

func TestExploreCatalogAdoptsCanonicalURLWithoutConflict(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	sourceID := insertCatalogSource(t, db, "https://discovery.example/profile", model.ExploreValidationPending, nil, nil)

	canonicalID, merged, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(sourceID, "https://feeds.example/profile.xml")
	if err != nil || canonicalID != sourceID || merged {
		t.Fatalf("adopt id=%d merged=%t err=%v", canonicalID, merged, err)
	}
	var feedURL, normalizedURL string
	var siteURL sql.NullString
	if err := db.QueryRow(`SELECT url,normalized_url,site_url FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&feedURL, &normalizedURL, &siteURL); err != nil {
		t.Fatal(err)
	}
	if feedURL != "https://feeds.example/profile.xml" || normalizedURL != feedURL || siteURL.String != "https://discovery.example/profile" {
		t.Fatalf("adopted url=%q normalized=%q site=%v", feedURL, normalizedURL, siteURL)
	}
}

func TestExploreCatalogSameURLResolvesOnlyDirectCanonicalTombstone(t *testing.T) {
	t.Run("ordinary source returns itself", func(t *testing.T) {
		db, cleanup := testdb.New(t)
		defer cleanup()
		feedURL := "https://same-url-ordinary.example/feed"
		sourceID := insertCatalogSource(t, db, feedURL, model.ExploreValidationPending, nil, nil)

		canonicalID, merged, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(sourceID, feedURL)
		if err != nil || canonicalID != sourceID || merged {
			t.Fatalf("ordinary same URL id=%d want=%d merged=%t err=%v", canonicalID, sourceID, merged, err)
		}
	})

	t.Run("direct tombstone returns root", func(t *testing.T) {
		db, cleanup := testdb.New(t)
		defer cleanup()
		aURL := "https://same-url-tombstone.example/a"
		bURL := "https://same-url-tombstone.example/b"
		aID := insertCatalogSource(t, db, aURL, model.ExploreValidationPending, nil, nil)
		bID := insertCatalogSource(t, db, bURL, model.ExploreValidationValid, nil, nil)
		repo := NewExploreCatalogRepository(db)
		if canonicalID, merged, err := repo.AdoptDiscoveredFeed(aID, bURL); err != nil || canonicalID != bID || !merged {
			t.Fatalf("stage tombstone id=%d want=%d merged=%t err=%v", canonicalID, bID, merged, err)
		}

		canonicalID, merged, err := repo.AdoptDiscoveredFeed(aID, aURL)
		if err != nil || canonicalID != bID || !merged {
			t.Fatalf("same URL tombstone id=%d want root=%d merged=%t err=%v", canonicalID, bID, merged, err)
		}
	})

	for _, tc := range []struct {
		name  string
		stage func(t *testing.T, db *sql.DB, aID, bID, cID int)
	}{
		{"chain", func(t *testing.T, db *sql.DB, aID, bID, cID int) {
			_, err := db.Exec(`UPDATE recommended_feeds SET merged_into_source_id=CASE id WHEN $1 THEN $2 WHEN $2 THEN $3 END WHERE id IN ($1,$2)`, aID, bID, cID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"cycle", func(t *testing.T, db *sql.DB, aID, bID, _ int) {
			_, err := db.Exec(`UPDATE recommended_feeds SET merged_into_source_id=CASE id WHEN $1 THEN $2 WHEN $2 THEN $1 END WHERE id IN ($1,$2)`, aID, bID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"dangling", func(t *testing.T, db *sql.DB, aID, _, _ int) {
			if _, err := db.Exec(`ALTER TABLE recommended_feeds DROP CONSTRAINT recommended_feeds_merged_into_source_id_fkey`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE recommended_feeds SET merged_into_source_id=2147483647 WHERE id=$1`, aID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name+" fails closed", func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			aURL := "https://same-url-conflict.example/" + tc.name + "/a"
			aID := insertCatalogSource(t, db, aURL, model.ExploreValidationInvalid, nil, nil)
			bID := insertCatalogSource(t, db, "https://same-url-conflict.example/"+tc.name+"/b", model.ExploreValidationInvalid, nil, nil)
			cID := insertCatalogSource(t, db, "https://same-url-conflict.example/"+tc.name+"/c", model.ExploreValidationValid, nil, nil)
			tc.stage(t, db, aID, bID, cID)

			if _, _, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(aID, aURL); !errors.Is(err, ErrExploreCanonicalAdoptionConflict) {
				t.Fatalf("same URL %s err=%v want canonical adoption conflict", tc.name, err)
			}
		})
	}
}

func TestExploreCatalogAdoptionJoinsOuterTransaction(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	sourceID := insertCatalogSource(t, db, "https://transaction.example/profile", model.ExploreValidationPending, nil, nil)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewExploreCatalogRepository(tx).AdoptDiscoveredFeed(sourceID, "https://transaction.example/feed"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var feedURL string
	if err := db.QueryRow(`SELECT url FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&feedURL); err != nil {
		t.Fatal(err)
	}
	if feedURL != "https://transaction.example/profile" {
		t.Fatalf("outer rollback retained adopted URL %q", feedURL)
	}
}

func TestExploreCatalogMergesCanonicalObservationsAndKeepsLoser(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	loserID := insertCatalogSource(t, db, "https://merge.example/discovered", model.ExploreValidationPending, nil, nil)
	targetID := insertCatalogSource(t, db, "https://merge.example/feed", model.ExploreValidationValid, &now, &now)
	if _, err := db.Exec(`UPDATE recommended_feeds SET title='canonical title',etag='target-etag',health_score=1,is_broken=false WHERE id=$1`, targetID); err != nil {
		t.Fatal(err)
	}
	providerID := insertCatalogProvider(t, db, "merge-provider", true)
	first := now.Add(-48 * time.Hour)
	last := now.Add(3 * time.Hour)
	if _, err := db.Exec(`
		INSERT INTO explore_source_observations
		(provider_id,source_id,external_key,provider_tags,first_seen_at,last_seen_at,occurrence_count)
		VALUES
		($1,$2,'same',ARRAY['go','rss'],$4,$5,4),
		($1,$3,'same',ARRAY['awesome','go'],$6,$7,7),
		($1,$3,'loser-only',ARRAY['independent'],$6,$7,2)`,
		providerID, targetID, loserID, now.Add(-24*time.Hour), now, first, last); err != nil {
		t.Fatal(err)
	}

	canonicalID, merged, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(loserID, "https://merge.example/feed")
	if err != nil || canonicalID != targetID || !merged {
		t.Fatalf("merge id=%d want=%d merged=%t err=%v", canonicalID, targetID, merged, err)
	}
	var tags pq.StringArray
	var firstSeen, lastSeen time.Time
	var count int
	if err := db.QueryRow(`
		SELECT provider_tags,first_seen_at,last_seen_at,occurrence_count
		FROM explore_source_observations
		WHERE provider_id=$1 AND source_id=$2 AND external_key='same'`, providerID, targetID).Scan(&tags, &firstSeen, &lastSeen, &count); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "awesome,go,rss" || !firstSeen.Equal(first) || !lastSeen.Equal(last) || count != 7 {
		t.Fatalf("merged tags=%v first=%v last=%v count=%d", tags, firstSeen, lastSeen, count)
	}
	var targetObservationCount, loserObservationCount int
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE source_id=$1`, targetID).Scan(&targetObservationCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE source_id=$1`, loserID).Scan(&loserObservationCount); err != nil {
		t.Fatal(err)
	}
	var loserStatus, loserError string
	var mergedIntoSourceID sql.NullInt64
	var loserHealth float64
	var loserBroken bool
	if err := db.QueryRow(`SELECT validation_status,health_score,is_broken,last_error,merged_into_source_id FROM recommended_feeds WHERE id=$1`, loserID).Scan(&loserStatus, &loserHealth, &loserBroken, &loserError, &mergedIntoSourceID); err != nil {
		t.Fatal(err)
	}
	var targetTitle, targetETag, targetStatus string
	var targetHealth float64
	if err := db.QueryRow(`SELECT title,etag,validation_status,health_score FROM recommended_feeds WHERE id=$1`, targetID).Scan(&targetTitle, &targetETag, &targetStatus, &targetHealth); err != nil {
		t.Fatal(err)
	}
	if targetObservationCount != 2 || loserObservationCount != 0 || loserStatus != model.ExploreValidationInvalid || loserHealth != 0 || !loserBroken || loserError != fmt.Sprintf("merged into explore source %d", targetID) || !mergedIntoSourceID.Valid || int(mergedIntoSourceID.Int64) != targetID {
		t.Fatalf("target obs=%d loser obs=%d loser status=%q health=%v broken=%t error=%q merged_into=%v", targetObservationCount, loserObservationCount, loserStatus, loserHealth, loserBroken, loserError, mergedIntoSourceID)
	}
	if targetTitle != "canonical title" || targetETag != "target-etag" || targetStatus != model.ExploreValidationValid || targetHealth != 1 {
		t.Fatalf("target degraded title=%q etag=%q status=%q health=%v", targetTitle, targetETag, targetStatus, targetHealth)
	}
}

func TestExploreCatalogAdoptsOrdinaryInvalidCanonicalTarget(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://ordinary-invalid.example/discovered", model.ExploreValidationPending, nil, nil)
	targetURL := "https://ordinary-invalid.example/feed"
	targetID := insertCatalogSource(t, db, targetURL, model.ExploreValidationInvalid, nil, nil)
	providerID := insertCatalogProvider(t, db, "ordinary-invalid-provider", true)
	insertCatalogObservation(t, db, providerID, sourceID, "source", now)

	canonicalID, merged, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(sourceID, targetURL)
	if err != nil || canonicalID != targetID || !merged {
		t.Fatalf("adopt ordinary invalid target id=%d want=%d merged=%t err=%v", canonicalID, targetID, merged, err)
	}
	var targetObservations int
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE source_id=$1`, targetID).Scan(&targetObservations); err != nil {
		t.Fatal(err)
	}
	var sourceMergedInto sql.NullInt64
	if err := db.QueryRow(`SELECT merged_into_source_id FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&sourceMergedInto); err != nil {
		t.Fatal(err)
	}
	if targetObservations != 1 || !sourceMergedInto.Valid || int(sourceMergedInto.Int64) != targetID {
		t.Fatalf("ordinary invalid target observations=%d source merged_into=%v", targetObservations, sourceMergedInto)
	}
}

func TestExploreCatalogConcurrentReverseAdoptionKeepsOneCanonicalSource(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(4)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	aURL := "https://reverse-adopt.example/a"
	bURL := "https://reverse-adopt.example/b"
	aID := insertCatalogSource(t, db, aURL, model.ExploreValidationPending, nil, nil)
	bID := insertCatalogSource(t, db, bURL, model.ExploreValidationPending, nil, nil)
	providerID := insertCatalogProvider(t, db, "reverse-adopt-provider", true)
	insertCatalogObservation(t, db, providerID, aID, "a", now)
	insertCatalogObservation(t, db, providerID, bID, "b", now)

	start := make(chan struct{})
	ids := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, request := range []struct {
		sourceID int
		target   string
	}{{aID, bURL}, {bID, aURL}} {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, _, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(request.sourceID, request.target)
			ids <- id
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("reverse adoption failed: %v", err)
		}
	}
	var returned []int
	for id := range ids {
		returned = append(returned, id)
	}
	if len(returned) != 2 || returned[0] != returned[1] {
		t.Fatalf("reverse adoption returned competing canonical ids: %v", returned)
	}

	var validOrPending, invalid, survivorObservations, loserObservations int
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE id IN ($1,$2) AND validation_status <> 'invalid'`, aID, bID).Scan(&validOrPending); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE id IN ($1,$2) AND validation_status = 'invalid'`, aID, bID).Scan(&invalid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE source_id=$1`, returned[0]).Scan(&survivorObservations); err != nil {
		t.Fatal(err)
	}
	loserID := aID
	if returned[0] == aID {
		loserID = bID
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_source_observations WHERE source_id=$1`, loserID).Scan(&loserObservations); err != nil {
		t.Fatal(err)
	}
	var loserMergedInto sql.NullInt64
	if err := db.QueryRow(`SELECT merged_into_source_id FROM recommended_feeds WHERE id=$1`, loserID).Scan(&loserMergedInto); err != nil {
		t.Fatal(err)
	}
	if validOrPending != 1 || invalid != 1 || survivorObservations != 2 || loserObservations != 0 || !loserMergedInto.Valid || int(loserMergedInto.Int64) != returned[0] {
		t.Fatalf("reverse adoption survivor=%d active=%d invalid=%d survivorObs=%d loserObs=%d loserMergedInto=%v", returned[0], validOrPending, invalid, survivorObservations, loserObservations, loserMergedInto)
	}
}

func TestExploreCatalogRegistryAndAdoptionConvergeConcurrently(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(4)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://race.example/discovery", model.ExploreValidationPending, nil, nil)
	providerID := insertCatalogProvider(t, db, "race-provider", true)
	start := make(chan struct{})
	ids := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		id, _, err := NewExploreCatalogRepository(db).AdoptDiscoveredFeed(sourceID, "https://race.example/feed")
		ids <- id
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		id, err := NewExploreRegistryRepository(db).UpsertCandidate(providerID, explore.Candidate{
			ExternalKey: "race", FeedURL: "https://race.example/feed", Title: "Race", Topic: "go", OccurrenceCount: 1,
		}, now)
		ids <- id
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var gotIDs []int
	for id := range ids {
		gotIDs = append(gotIDs, id)
	}
	if len(gotIDs) != 2 || gotIDs[0] != gotIDs[1] {
		t.Fatalf("concurrent canonical ids=%v", gotIDs)
	}
	var canonicalRows int
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE normalized_url='https://race.example/feed'`).Scan(&canonicalRows); err != nil {
		t.Fatal(err)
	}
	if canonicalRows != 1 {
		t.Fatalf("canonical rows=%d", canonicalRows)
	}
}

func TestExploreCatalogCheckedAtFenceRejectsStaleOutcomes(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://fence.example/feed", model.ExploreValidationPending, nil, nil)
	repo := NewExploreCatalogRepository(db)
	if err := repo.RecordFetchSuccess(sourceID, now, "new-etag", "new-modified"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordFetchFailure(sourceID, now.Add(-time.Minute), errors.New("stale failure")); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkValidationInvalid(sourceID, now.Add(-time.Second), errors.New("stale invalid")); err != nil {
		t.Fatal(err)
	}
	var status, etag string
	var health float64
	var broken bool
	var lastError sql.NullString
	var checkedAt time.Time
	if err := db.QueryRow(`SELECT validation_status,etag,health_score,is_broken,last_error,last_checked_at FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &etag, &health, &broken, &lastError, &checkedAt); err != nil {
		t.Fatal(err)
	}
	if status != model.ExploreValidationValid || etag != "new-etag" || health != 1 || broken || lastError.Valid || !checkedAt.Equal(now) {
		t.Fatalf("stale failure overwrote success status=%q etag=%q health=%v broken=%t error=%v checked=%v", status, etag, health, broken, lastError, checkedAt)
	}
	terminalAt := now.Add(2 * time.Hour)
	if err := repo.MarkValidationInvalid(sourceID, terminalAt, errors.New("terminal")); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordFetchSuccess(sourceID, terminalAt.Add(-time.Minute), "stale-success", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkValidationValid(sourceID, terminalAt.Add(-time.Second), "stale-valid", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT validation_status,etag,health_score,is_broken,last_error,last_checked_at FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&status, &etag, &health, &broken, &lastError, &checkedAt); err != nil {
		t.Fatal(err)
	}
	if status != model.ExploreValidationInvalid || health != 0 || !broken || lastError.String != "terminal" || !checkedAt.Equal(terminalAt) {
		t.Fatalf("stale success overwrote terminal status=%q etag=%q health=%v broken=%t error=%v checked=%v", status, etag, health, broken, lastError, checkedAt)
	}
	missingID := sourceID + 100000
	if err := repo.RecordFetchFailure(missingID, now, errors.New("missing")); err == nil || err.Error() != exploreSourceNotFoundError(missingID).Error() {
		t.Fatalf("missing stale-aware mutation err=%v", err)
	}
}

func TestExploreCatalogLoadsCanonicalSourceAndProviderObservations(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	sourceID := insertCatalogSource(t, db, "https://catalog.example/feed", model.ExploreValidationPending, nil, nil)

	providerIDs := make([]int, 2)
	for index, fixture := range []struct {
		key, kind string
		enabled   bool
	}{{"catalog-opml", model.ExploreProviderKindOPML, true}, {"catalog-reddit", model.ExploreProviderKindRedditStream, false}} {
		if err := db.QueryRow(`
			INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, topic, enabled)
			VALUES ($1, $2, $3, 'go', $4) RETURNING id`, fixture.key, fixture.kind, "https://registry.example/"+fixture.key, fixture.enabled).Scan(&providerIDs[index]); err != nil {
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
	if catalog.Observations[0].ProviderEnabled || !catalog.Observations[1].ProviderEnabled {
		t.Fatalf("disabled observation was hidden or mislabeled: %+v", catalog.Observations)
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
	noObservationID := insertCatalogSource(t, db, "https://no-observation.example/feed", model.ExploreValidationPending, &old, nil)
	disabledOnlyID := insertCatalogSource(t, db, "https://disabled-only.example/feed", model.ExploreValidationPending, &old, nil)
	enabledProviderID := insertCatalogProvider(t, db, "due-enabled", true)
	disabledProviderID := insertCatalogProvider(t, db, "due-disabled", false)
	insertCatalogObservation(t, db, enabledProviderID, pendingID, "pending-enabled", old)
	insertCatalogObservation(t, db, enabledProviderID, validID, "valid-enabled", old)
	insertCatalogObservation(t, db, disabledProviderID, disabledOnlyID, "disabled-only", old)

	due, err := NewExploreCatalogRepository(db).ListDueSources(now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 || due[0].ID != pendingID || due[1].ID != validID {
		t.Fatalf("due=%+v invalidIDs=%d,%d noObservationID=%d disabledOnlyID=%d", due, invalidID, oldInvalidID, noObservationID, disabledOnlyID)
	}
	limited, err := NewExploreCatalogRepository(db).ListDueSources(now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-24*time.Hour), 1)
	if err != nil || len(limited) != 1 || limited[0].ID != pendingID {
		t.Fatalf("limited=%+v err=%v", limited, err)
	}
	insertCatalogObservation(t, db, enabledProviderID, disabledOnlyID, "new-enabled", fresh)
	due, err = NewExploreCatalogRepository(db).ListDueSources(now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-24*time.Hour), 10)
	if err != nil || len(due) != 3 || due[1].ID != disabledOnlyID {
		t.Fatalf("new enabled observation did not restore source: due=%+v err=%v", due, err)
	}
}

func TestExploreCatalogTransitionsHealthWithoutDeletingLastGoodCache(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://health.example/feed", model.ExploreValidationPending, nil, nil)
	providerID := insertCatalogProvider(t, db, "health-enabled", true)
	insertCatalogObservation(t, db, providerID, sourceID, "health-source", now)
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
	notDue, err := repo.ListDueSources(now.Add(24*time.Hour), now.Add(3*time.Hour+59*time.Minute), now.Add(-20*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notDue) != 0 {
		t.Fatalf("failed source immediately due again: %+v", notDue)
	}
	notDue, err = repo.ListDueSources(now.Add(24*time.Hour), now.Add(4*time.Hour+time.Second), now.Add(-19*time.Hour), 10)
	if err != nil || len(notDue) != 0 {
		t.Fatalf("broken source entered normal refresh cadence: due=%+v err=%v", notDue, err)
	}
	due, err := repo.ListDueSources(now.Add(48*time.Hour), now.Add(48*time.Hour), now.Add(4*time.Hour+time.Second), 10)
	if err != nil || len(due) != 1 || due[0].ID != sourceID {
		t.Fatalf("source not due after daily health interval: due=%+v err=%v", due, err)
	}

	rediscoveredAt := now.Add(4*time.Hour + 2*time.Second)
	insertCatalogObservation(t, db, providerID, sourceID, "health-source-rediscovered", rediscoveredAt)
	due, err = repo.ListDueSources(now.Add(24*time.Hour), now.Add(5*time.Hour), now.Add(-19*time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].ID != sourceID {
		t.Fatalf("fresh observation did not restore early revalidation: due=%+v err=%v", due, err)
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

func TestExploreCatalogConditionalRequestValidatorState(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	sourceID := insertCatalogSource(t, db, "https://validators.example/feed", model.ExploreValidationPending, nil, nil)
	repo := NewExploreCatalogRepository(db)
	if _, err := db.Exec(`UPDATE recommended_feeds SET etag='stale-tag',last_modified='stale-date' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkValidationValid(sourceID, now, "", ""); err != nil {
		t.Fatal(err)
	}
	var etag, modified sql.NullString
	if err := db.QueryRow(`SELECT etag,last_modified FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&etag, &modified); err != nil {
		t.Fatal(err)
	}
	if etag.Valid || modified.Valid {
		t.Fatalf("200 without validators retained stale state: etag=%v modified=%v", etag, modified)
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET etag='keep-tag',last_modified='keep-date',health_score=0,is_broken=true WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	notModifiedAt := now.Add(time.Hour)
	if err := repo.RecordFetchNotModified(sourceID, notModifiedAt); err != nil {
		t.Fatal(err)
	}
	var checkedAt, fetchedAt time.Time
	var score float64
	var broken bool
	if err := db.QueryRow(`SELECT etag,last_modified,last_checked_at,last_fetched_at,health_score,is_broken FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&etag, &modified, &checkedAt, &fetchedAt, &score, &broken); err != nil {
		t.Fatal(err)
	}
	if etag.String != "keep-tag" || modified.String != "keep-date" || !checkedAt.Equal(notModifiedAt) || !fetchedAt.Equal(notModifiedAt) || score != 1 || broken {
		t.Fatalf("304 state etag=%v modified=%v checked=%v fetched=%v score=%v broken=%t", etag, modified, checkedAt, fetchedAt, score, broken)
	}
	if err := repo.RecordFetchSuccess(sourceID, now.Add(2*time.Hour), "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT etag,last_modified FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&etag, &modified); err != nil {
		t.Fatal(err)
	}
	if etag.Valid || modified.Valid {
		t.Fatalf("refresh 200 without validators retained stale state: etag=%v modified=%v", etag, modified)
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
	staleContent, staleExcerpt := "stale content", "stale excerpt"
	stale := first
	stale.Title, stale.URL, stale.Content, stale.Excerpt = "stale title", "https://low-frequency.example/stale", &staleContent, &staleExcerpt
	stale.FetchedAt = now.Add(-time.Hour)
	stale.PublishedAt = ptrTime(now)
	staleID, err := repo.UpsertArticle(stale)
	if err != nil || staleID != firstID {
		t.Fatalf("older upsert id=%d want=%d err=%v", staleID, firstID, err)
	}
	if err := db.QueryRow(`SELECT title,url,content,excerpt,published_at,fetched_at FROM explore_articles WHERE id=$1`, firstID).Scan(&gotTitle, &gotURL, &gotContent, &gotExcerpt, &gotPublished, &stale.FetchedAt); err != nil {
		t.Fatal(err)
	}
	if gotTitle != "updated" || gotURL != first.URL || gotContent.String != "" || gotExcerpt.String != "" || !gotPublished.Equal(oldPublished) || !stale.FetchedAt.Equal(now) {
		t.Fatalf("older result overwrote newer row title=%q url=%q content=%v excerpt=%v published=%v fetched=%v", gotTitle, gotURL, gotContent, gotExcerpt, gotPublished, stale.FetchedAt)
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

func TestExploreCatalogStandaloneRetentionDistinguishesMissingAndEmptySource(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repo := NewExploreCatalogRepository(db)
	emptySourceID := insertCatalogSource(t, db, "https://empty-retention.example/feed", model.ExploreValidationValid, &now, &now)
	if err := repo.RetainArticles(emptySourceID, now); err != nil {
		t.Fatalf("existing empty source retention: %v", err)
	}
	missingSourceID := emptySourceID + 100000
	if err := repo.RetainArticles(missingSourceID, now); err == nil || err.Error() != fmt.Sprintf("explore source %d not found", missingSourceID) {
		t.Fatalf("missing source retention err=%v", err)
	}
}

func insertCatalogSource(t *testing.T, db *sql.DB, feedURL, status string, checkedAt, fetchedAt *time.Time) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO recommended_feeds
		(url,title,category,language,normalized_url,validation_status,last_checked_at,last_fetched_at,health_score)
		VALUES ($1,$1,'test','en',$1,$2::varchar,$3,$4,CASE WHEN $2::text='valid' THEN 1 ELSE NULL END)
		RETURNING id`, feedURL, status, checkedAt, fetchedAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCatalogProvider(t *testing.T, db *sql.DB, key string, enabled bool) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`
		INSERT INTO explore_registry_providers (provider_key,provider_kind,endpoint,enabled)
		VALUES ($1,'opml',$2,$3) RETURNING id`, key, "https://registry.example/"+key, enabled).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCatalogObservation(t *testing.T, db *sql.DB, providerID, sourceID int, externalKey string, observedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO explore_source_observations
		(provider_id,source_id,external_key,first_seen_at,last_seen_at)
		VALUES ($1,$2,$3,$4,$4)`, providerID, sourceID, externalKey, observedAt); err != nil {
		t.Fatal(err)
	}
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
