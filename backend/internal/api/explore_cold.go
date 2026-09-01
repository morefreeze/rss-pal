package api

import (
	"database/sql"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

const coldStartStaleAfter = 45 * time.Minute

var ErrExploreColdStartPending = errors.New("explore cold start has no validated candidates yet")

type exploreColdRankLoader interface {
	LoadColdCandidates(time.Time) ([]explorelogic.RankCandidate, error)
	LoadColdFeedback(int) ([]explorelogic.ExplicitFeedbackInput, error)
}

type exploreColdSnapshotStore interface {
	LatestDone(int) (*model.ExploreBatch, []model.ExploreBatchSource, error)
	Claim(int, time.Time, time.Time, time.Duration) (*repository.ExploreSnapshotClaim, bool, error)
	Publish(int, repository.ExploreSnapshotGenerationToken, []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error)
	Fail(int, repository.ExploreSnapshotGenerationToken, error) error
}

type ExploreColdStartService struct {
	snapshots exploreColdSnapshotStore
	ranks     exploreColdRankLoader
	logger    *log.Logger
}

func NewExploreColdStartService(snapshots exploreColdSnapshotStore, ranks exploreColdRankLoader, logger *log.Logger) *ExploreColdStartService {
	if logger == nil {
		logger = log.Default()
	}
	return &ExploreColdStartService{snapshots: snapshots, ranks: ranks, logger: logger}
}

func (service *ExploreColdStartService) Ensure(userID int, now time.Time) error {
	if service == nil || service.snapshots == nil || service.ranks == nil || userID <= 0 || now.IsZero() {
		return errors.New("explore cold start dependencies are required")
	}
	if _, _, err := service.snapshots.LatestDone(userID); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	candidates, err := service.ranks.LoadColdCandidates(now)
	if err != nil {
		return err
	}
	feedback, err := service.ranks.LoadColdFeedback(userID)
	if err != nil {
		return err
	}
	profile := explorelogic.BuildExploreProfile(explorelogic.ProfileInput{Now: now, Feedback: feedback})
	ranked := explorelogic.RankExploreCandidates(profile, candidates, now)
	claim, owned, err := service.snapshots.Claim(userID, repository.ExploreColdStartSlotAt, now, coldStartStaleAfter)
	if err != nil || !owned || claim == nil {
		return err
	}
	if len(ranked) == 0 {
		return ErrExploreColdStartPending
	}
	values := make([]repository.ExploreSnapshotSourceInput, len(ranked))
	for index, candidate := range ranked {
		values[index] = repository.ExploreSnapshotSourceInput{SourceID: candidate.SourceID, Score: candidate.Score, Topic: candidate.Topic, Reason: candidate.Reason}
	}
	if _, err := service.snapshots.Publish(claim.Batch.ID, claim.GenerationToken, values); err != nil {
		if failErr := service.snapshots.Fail(claim.Batch.ID, claim.GenerationToken, err); failErr != nil {
			service.logger.Printf("explore cold_start batch_id=%d user_id=%d fail_write_error=true", claim.Batch.ID, userID)
		}
		return err
	}
	return nil
}

type SQLExploreColdRankLoader struct{ db repository.Querier }

func NewSQLExploreColdRankLoader(db repository.Querier) *SQLExploreColdRankLoader {
	return &SQLExploreColdRankLoader{db: db}
}

func (loader *SQLExploreColdRankLoader) WithCtx(c ctxkey.CtxGetter) *SQLExploreColdRankLoader {
	if value, ok := c.Get(ctxkey.Tx); ok {
		if db, ok := value.(repository.Querier); ok {
			return NewSQLExploreColdRankLoader(db)
		}
	}
	return loader
}

func (loader *SQLExploreColdRankLoader) LoadColdFeedback(userID int) ([]explorelogic.ExplicitFeedbackInput, error) {
	rows, err := loader.db.Query(`SELECT COALESCE(source_id,0),COALESCE(topic,''),feedback_type FROM explore_feedback WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []explorelogic.ExplicitFeedbackInput{}
	for rows.Next() {
		var item explorelogic.ExplicitFeedbackInput
		if err := rows.Scan(&item.SourceID, &item.Topic, &item.Type); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (loader *SQLExploreColdRankLoader) LoadColdCandidates(now time.Time) ([]explorelogic.RankCandidate, error) {
	rows, err := loader.db.Query(`
		SELECT source.id,source.title,source.category,COALESCE(source.site_url,source.url),
		       source.validation_status,source.is_broken,source.merged_into_source_id,COALESCE(source.health_score,0)
		FROM recommended_feeds source
		WHERE source.validation_status='valid' AND source.is_broken=false AND source.merged_into_source_id IS NULL
		  AND EXISTS (
		      SELECT 1 FROM explore_source_observations observation
		      JOIN explore_registry_providers provider ON provider.id=observation.provider_id
		      WHERE observation.source_id=source.id AND provider.enabled
		        AND observation.last_seen_at >= $1 - GREATEST(provider.sync_interval_minutes * 2 * INTERVAL '1 minute', INTERVAL '6 hours')
		  )
		ORDER BY COALESCE(source.health_score,0) DESC,source.last_observed_at DESC NULLS LAST,source.id
		LIMIT 2000`, now)
	if err != nil {
		return nil, err
	}
	candidates := []explorelogic.RankCandidate{}
	ids, byID := []int{}, map[int]int{}
	for rows.Next() {
		var candidate explorelogic.RankCandidate
		var rawURL string
		if err := rows.Scan(&candidate.SourceID, &candidate.Title, &candidate.Category, &rawURL, &candidate.ValidationStatus, &candidate.IsBroken, &candidate.MergedIntoSourceID, &candidate.HealthScore); err != nil {
			rows.Close()
			return nil, err
		}
		candidate.Domain = coldURLDomain(rawURL)
		candidate.Topic = candidate.Category
		byID[candidate.SourceID] = len(candidates)
		ids = append(ids, candidate.SourceID)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return candidates, nil
	}
	rows, err = loader.db.Query(`
		SELECT observation.source_id,provider.provider_key,COALESCE(provider.topic,''),observation.provider_tags,observation.last_seen_at
		FROM explore_source_observations observation JOIN explore_registry_providers provider ON provider.id=observation.provider_id
		WHERE observation.source_id=ANY($1) AND provider.enabled
		  AND observation.last_seen_at >= $2 - GREATEST(provider.sync_interval_minutes * 2 * INTERVAL '1 minute', INTERVAL '6 hours')
		ORDER BY observation.source_id,observation.last_seen_at DESC,observation.id`, pq.Array(ids), now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sourceID int
		var observation explorelogic.RankObservation
		if err := rows.Scan(&sourceID, &observation.Provider, &observation.Topic, pq.Array(&observation.Tags), &observation.LastObservedAt); err != nil {
			rows.Close()
			return nil, err
		}
		index := byID[sourceID]
		candidates[index].Observations = append(candidates[index].Observations, observation)
		if candidates[index].Provider == "" {
			candidates[index].Provider = observation.Provider
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = loader.db.Query(`
		SELECT article.source_id,article.id,article.title,article.published_at,article.fetched_at
		FROM recommended_feeds source JOIN LATERAL (
			SELECT id,source_id,title,published_at,fetched_at FROM explore_articles
			WHERE source_id=source.id
			ORDER BY COALESCE(published_at,fetched_at) DESC,fetched_at DESC,id DESC LIMIT 5
		) article ON true WHERE source.id=ANY($1)
		ORDER BY article.source_id,COALESCE(article.published_at,article.fetched_at) DESC,article.id DESC`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID int
		var article explorelogic.RankArticle
		var published sql.NullTime
		if err := rows.Scan(&sourceID, &article.ID, &article.Title, &published, &article.FetchedAt); err != nil {
			return nil, err
		}
		if published.Valid {
			article.PublishedAt = published.Time
		}
		candidates[byID[sourceID]].Articles = append(candidates[byID[sourceID]].Articles, article)
	}
	return candidates, rows.Err()
}

func coldURLDomain(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
