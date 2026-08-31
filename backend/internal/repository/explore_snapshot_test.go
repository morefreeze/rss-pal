package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestValidateExploreSnapshotSources(t *testing.T) {
	valid := []ExploreSnapshotSourceInput{{SourceID: 1, Score: 1.25, Topic: "编程", Reason: "与你订阅的 Go 相关"}}
	tooMany := make([]ExploreSnapshotSourceInput, MaxExploreSnapshotSources+1)
	for i := range tooMany {
		tooMany[i] = ExploreSnapshotSourceInput{SourceID: i + 1}
	}

	for _, tc := range []struct {
		name    string
		values  []ExploreSnapshotSourceInput
		wantErr bool
	}{
		{name: "valid", values: valid},
		{name: "empty is valid", values: nil},
		{name: "at most twelve", values: tooMany, wantErr: true},
		{name: "positive source", values: []ExploreSnapshotSourceInput{{SourceID: 0}}, wantErr: true},
		{name: "unique source", values: []ExploreSnapshotSourceInput{{SourceID: 1}, {SourceID: 1}}, wantErr: true},
		{name: "finite score NaN", values: []ExploreSnapshotSourceInput{{SourceID: 1, Score: math.NaN()}}, wantErr: true},
		{name: "finite score Inf", values: []ExploreSnapshotSourceInput{{SourceID: 1, Score: math.Inf(1)}}, wantErr: true},
		{name: "topic rune bound", values: []ExploreSnapshotSourceInput{{SourceID: 1, Topic: strings.Repeat("界", MaxExploreSnapshotTopicRunes+1)}}, wantErr: true},
		{name: "reason rune bound", values: []ExploreSnapshotSourceInput{{SourceID: 1, Reason: strings.Repeat("界", MaxExploreSnapshotReasonRunes+1)}}, wantErr: true},
		{name: "topic invalid utf8", values: []ExploreSnapshotSourceInput{{SourceID: 1, Topic: string([]byte{0xff})}}, wantErr: true},
		{name: "reason invalid utf8", values: []ExploreSnapshotSourceInput{{SourceID: 1, Reason: string([]byte{0xff})}}, wantErr: true},
		{name: "topic nul", values: []ExploreSnapshotSourceInput{{SourceID: 1, Topic: "topic\x00tail"}}, wantErr: true},
		{name: "reason nul", values: []ExploreSnapshotSourceInput{{SourceID: 1, Reason: "reason\x00tail"}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExploreSnapshotSources(tc.values)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func TestExploreSnapshotLatestDoneLocksBatchForConsistentSources(t *testing.T) {
	if !strings.Contains(exploreLatestDoneBatchSQL, "FOR SHARE") {
		t.Fatalf("latest done batch query is not locked: %s", exploreLatestDoneBatchSQL)
	}
}

func TestNewExploreGenerationTokenIsOpaqueAndUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		token, err := newExploreGenerationToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != 64 {
			t.Fatalf("token length=%d", len(token))
		}
		if _, err := hex.DecodeString(token); err != nil {
			t.Fatalf("token is not hex: %q: %v", token, err)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatalf("duplicate generation token %q", token)
		}
		seen[token] = struct{}{}
	}
}

func TestExploreSnapshotClaimTokenNeverSerializes(t *testing.T) {
	encoded, err := json.Marshal(ExploreSnapshotClaim{GenerationToken: "top-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") || strings.Contains(string(encoded), "GenerationToken") {
		t.Fatalf("generation token leaked through JSON: %s", encoded)
	}
}

func TestExploreSnapshotMigrationDefinesGenerationFence(t *testing.T) {
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "038_subscription_explore.sql"))
	if err != nil {
		t.Fatal(err)
	}
	definition := string(migration)
	for _, fragment := range []string{
		"generation_token VARCHAR(64)",
		"started_at TIMESTAMP",
		"UNIQUE (batch_id, rank)",
		"reason VARCHAR(500)",
	} {
		if !strings.Contains(definition, fragment) {
			t.Errorf("snapshot migration missing %q", fragment)
		}
	}
}

func TestExploreSnapshotRepositoryWithQuerierAndCtx(t *testing.T) {
	raw := &sql.DB{}
	tx := &sql.Tx{}
	repo := NewExploreSnapshotRepository(raw)
	bound := repo.WithQuerier(tx)
	if bound == repo || bound.db != tx || bound.rawDB != raw {
		t.Fatalf("WithQuerier did not preserve raw database: repo=%+v bound=%+v", repo, bound)
	}
	if got := repo.WithCtx(fakeCtx{ctxkey.Tx: Querier(tx)}); got.db != tx || got.rawDB != raw {
		t.Fatalf("WithCtx did not bind transaction: %+v", got)
	}
	if repo.WithCtx(fakeCtx{}) != repo {
		t.Fatal("WithCtx without transaction must return receiver")
	}
}

func TestExploreSnapshotClaimHasSingleOwnerAndRotatesOnlyStaleOrFailed(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID := seedExploreSnapshotUser(t, db, "snapshot-claim")
	repo := NewExploreSnapshotRepository(db)
	slot := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	now := slot.Add(time.Minute)

	const contenders = 8
	start := make(chan struct{})
	type claimResult struct {
		claim    *ExploreSnapshotClaim
		acquired bool
	}
	results := make(chan claimResult, contenders)
	errs := make(chan error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claim, acquired, err := repo.Claim(userID, slot, now, time.Hour)
			results <- claimResult{claim: claim, acquired: acquired}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	owners := 0
	var originalToken string
	var batchID int
	for result := range results {
		if result.acquired {
			owners++
			originalToken = result.claim.GenerationToken
			batchID = result.claim.Batch.ID
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent claim: %v", err)
		}
	}
	if owners != 1 {
		t.Fatalf("owners=%d want=1", owners)
	}

	first, acquired, err := repo.Claim(userID, slot, now.Add(30*time.Minute), time.Hour)
	if err != nil || acquired || first.GenerationToken != "" {
		t.Fatalf("fresh claim leaked ownership: claim=%+v acquired=%t err=%v", first, acquired, err)
	}
	stale, acquired, err := repo.Claim(userID, slot, now.Add(2*time.Hour), time.Hour)
	if err != nil || !acquired || stale.GenerationToken == "" {
		t.Fatalf("stale adoption claim=%+v acquired=%t err=%v", stale, acquired, err)
	}
	if stale.GenerationToken == originalToken {
		t.Fatal("stale adoption did not rotate generation token")
	}
	if _, err := repo.Publish(batchID, originalToken, nil); !errors.Is(err, ErrExploreSnapshotFence) {
		t.Fatalf("stale owner published with old token: %v", err)
	}
	if err := repo.Fail(batchID, originalToken, errors.New("stale owner")); !errors.Is(err, ErrExploreSnapshotFence) {
		t.Fatalf("stale owner failed with old token: %v", err)
	}
	if err := repo.Fail(stale.Batch.ID, stale.GenerationToken, errors.New("generation failed")); err != nil {
		t.Fatal(err)
	}
	var tokenCleared bool
	if err := db.QueryRow(`SELECT generation_token IS NULL FROM explore_batches WHERE id=$1`, stale.Batch.ID).Scan(&tokenCleared); err != nil || !tokenCleared {
		t.Fatalf("failed batch retained token cleared=%t err=%v", tokenCleared, err)
	}
	var storedError string
	longErrorClaim, acquired, err := repo.Claim(userID, slot.Add(3*time.Hour), now.Add(3*time.Hour), time.Hour)
	if err != nil || !acquired {
		t.Fatalf("long error claim=%+v acquired=%t err=%v", longErrorClaim, acquired, err)
	}
	if err := repo.Fail(longErrorClaim.Batch.ID, longErrorClaim.GenerationToken, errors.New(strings.Repeat("界", MaxExploreSnapshotErrorRunes+100))); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT error_message FROM explore_batches WHERE id=$1`, longErrorClaim.Batch.ID).Scan(&storedError); err != nil || len([]rune(storedError)) != MaxExploreSnapshotErrorRunes {
		t.Fatalf("stored error runes=%d err=%v", len([]rune(storedError)), err)
	}
	retry, acquired, err := repo.Claim(userID, slot, now.Add(2*time.Hour+time.Minute), time.Hour)
	if err != nil || !acquired || retry.GenerationToken == stale.GenerationToken {
		t.Fatalf("failed adoption claim=%+v acquired=%t err=%v", retry, acquired, err)
	}
}

func TestExploreSnapshotPublishIsAtomicFencedAndLatestDoneWins(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID := seedExploreSnapshotUser(t, db, "snapshot-publish")
	validA := seedExploreSnapshotSource(t, db, "https://snapshot-a.example/feed", "valid", false, nil)
	validB := seedExploreSnapshotSource(t, db, "https://snapshot-b.example/feed", "valid", false, nil)
	invalid := seedExploreSnapshotSource(t, db, "https://snapshot-invalid.example/feed", "invalid", false, nil)
	broken := seedExploreSnapshotSource(t, db, "https://snapshot-broken.example/feed", "valid", true, nil)
	merged := seedExploreSnapshotSource(t, db, "https://snapshot-merged.example/feed", "valid", false, &validA)
	repo := NewExploreSnapshotRepository(db)
	now := time.Date(2026, 8, 31, 8, 1, 0, 0, time.UTC)
	if batch, sources, err := repo.LatestDone(userID); !errors.Is(err, sql.ErrNoRows) || batch != nil || sources != nil {
		t.Fatalf("empty latest batch=%+v sources=%+v err=%v", batch, sources, err)
	}

	claim, acquired, err := repo.Claim(userID, now.Truncate(time.Hour), now, time.Hour)
	if err != nil || !acquired {
		t.Fatalf("claim=%+v acquired=%t err=%v", claim, acquired, err)
	}
	if _, err := repo.Publish(claim.Batch.ID, "wrong-token", []ExploreSnapshotSourceInput{{SourceID: validA}}); !errors.Is(err, ErrExploreSnapshotFence) {
		t.Fatalf("wrong-token publish error=%v", err)
	}
	for name, sourceID := range map[string]int{"invalid": invalid, "broken": broken, "merged": merged} {
		if _, err := repo.Publish(claim.Batch.ID, claim.GenerationToken, []ExploreSnapshotSourceInput{{SourceID: validA}, {SourceID: sourceID}}); !errors.Is(err, ErrInvalidExploreSnapshot) {
			t.Fatalf("%s source publish error=%v", name, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM explore_batch_sources WHERE batch_id=$1`, claim.Batch.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid publish leaked rows count=%d err=%v", count, err)
	}
	batch, err := repo.Publish(claim.Batch.ID, claim.GenerationToken, []ExploreSnapshotSourceInput{
		{SourceID: validB, Score: 2, Topic: "second", Reason: "second reason"},
		{SourceID: validA, Score: 1, Topic: "first", Reason: "first reason"},
	})
	if err != nil || batch.Status != model.ExploreBatchDone || batch.SourceCount != 2 {
		t.Fatalf("publish batch=%+v err=%v", batch, err)
	}
	var tokenCleared bool
	if err := db.QueryRow(`SELECT generation_token IS NULL FROM explore_batches WHERE id=$1`, batch.ID).Scan(&tokenCleared); err != nil || !tokenCleared {
		t.Fatalf("done batch retained token cleared=%t err=%v", tokenCleared, err)
	}
	if _, err := repo.Publish(claim.Batch.ID, claim.GenerationToken, nil); !errors.Is(err, ErrExploreSnapshotFence) {
		t.Fatalf("done batch reopened: %v", err)
	}
	if err := repo.Fail(claim.Batch.ID, claim.GenerationToken, errors.New("late failure")); !errors.Is(err, ErrExploreSnapshotFence) {
		t.Fatalf("done batch failed: %v", err)
	}
	if _, acquired, err := repo.Claim(userID, claim.Batch.SlotAt, now.Add(24*time.Hour), time.Hour); err != nil || acquired {
		t.Fatalf("done claim acquired=%t err=%v", acquired, err)
	}

	newer, acquired, err := repo.Claim(userID, claim.Batch.SlotAt.Add(3*time.Hour), now.Add(3*time.Hour), time.Hour)
	if err != nil || !acquired {
		t.Fatalf("newer claim=%+v acquired=%t err=%v", newer, acquired, err)
	}
	latest, sources, err := repo.LatestDone(userID)
	if err != nil || latest.ID != claim.Batch.ID {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	if got := []int{sources[0].SourceID, sources[1].SourceID}; !reflect.DeepEqual(got, []int{validB, validA}) || sources[0].Rank != 1 || sources[1].Rank != 2 {
		t.Fatalf("ranked sources=%+v", sources)
	}
	if err := repo.Fail(newer.Batch.ID, newer.GenerationToken, errors.New("newer failed")); err != nil {
		t.Fatal(err)
	}
	latest, _, err = repo.LatestDone(userID)
	if err != nil || latest.ID != claim.Batch.ID {
		t.Fatalf("failed batch hid latest done: latest=%+v err=%v", latest, err)
	}
}

func TestExploreSnapshotCleanupUsesBatchAndEventRetentionWindows(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	userID := seedExploreSnapshotUser(t, db, "snapshot-cleanup")
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO explore_batches (user_id,slot_at,status,created_at) VALUES ($1,$2,'failed',$2),($1,$3,'failed',$3)`, userID, now.Add(-31*24*time.Hour), now.Add(-29*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_article_events (user_id,event_type,occurred_at) VALUES ($1,'click',$2),($1,'click',$3)`, userID, now.Add(-181*24*time.Hour), now.Add(-179*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	batches, events, err := NewExploreSnapshotRepository(db).Cleanup(now)
	if err != nil || batches != 1 || events != 1 {
		t.Fatalf("cleanup batches=%d events=%d err=%v", batches, events, err)
	}
}

func TestExploreSnapshotRepositoryRespectsRLSContext(t *testing.T) {
	privDB, schema, cleanupSchema := testdb.NewWithSchema(t)
	defer cleanupSchema()
	appDB, cleanupApp := testdb.NewAsApp(t, schema)
	defer cleanupApp()
	userA := seedExploreSnapshotUser(t, privDB, "snapshot-rls-a")
	userB := seedExploreSnapshotUser(t, privDB, "snapshot-rls-b")

	tx, err := appDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('app.user_id',$1,true)`, userA); err != nil {
		t.Fatal(err)
	}
	repo := NewExploreSnapshotRepository(appDB).WithCtx(fakeCtx{ctxkey.Tx: Querier(tx)})
	if _, _, err := repo.Claim(userB, time.Now().UTC().Truncate(time.Hour), time.Now().UTC(), time.Hour); err == nil {
		t.Fatal("RLS allowed user A to claim user B snapshot")
	}
}

func seedExploreSnapshotUser(t *testing.T, db *sql.DB, username string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`INSERT INTO users (username,password_hash) VALUES ($1,'x') RETURNING id`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedExploreSnapshotSource(t *testing.T, db *sql.DB, rawURL, status string, broken bool, merged *int) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds (url,title,category,language,normalized_url,validation_status,is_broken,merged_into_source_id) VALUES ($1,$1,'test','en',$1,$2,$3,$4) RETURNING id`, rawURL, status, broken, merged).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
