package explore

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestExploreRankSubscriptionOutweighsBehaviorAndFeedbackOutweighsBehavior(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{
		Now:           now,
		Subscriptions: []SubscriptionSignalInput{{Title: "Go", Category: "Programming", Tags: []string{"backend"}}},
		ExploreEvents: []ExploreEventSignalInput{
			{Topic: "photography", EventType: ExploreEventCompletedRead, OccurredAt: now.Add(-time.Hour)},
			{Topic: "photography", EventType: ExploreEventCompletedRead, OccurredAt: now.Add(-2 * time.Hour)},
		},
		Feedback: []ExplicitFeedbackInput{{Topic: "photography", Type: FeedbackDampenTopic}, {Topic: "gardening", Type: FeedbackBoostTopic}},
	})
	candidates := []RankCandidate{
		rankFixture(1, "go", []string{"backend"}, "directory-a", now),
		rankFixture(2, "photography", nil, "directory-b", now),
		rankFixture(3, "gardening", nil, "directory-c", now),
	}
	ranked := RankExploreCandidates(profile, candidates, now)
	if got := rankedSourceIDs(ranked); !reflect.DeepEqual(got, []int{1, 3, 2}) {
		t.Fatalf("ranked=%v scores=%v", got, rankedScores(ranked))
	}
	if ranked[0].Reason != "与你订阅的 backend 相关" {
		t.Fatalf("subscription reason=%q", ranked[0].Reason)
	}
}

func TestExploreRankRecentFormalArticleMetadataOutweighsRepeatedExploreBehavior(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	input := ProfileInput{
		Now:            now,
		RecentArticles: []RecentArticleSignalInput{{Title: "Systems weekly", Tags: []string{"backend"}, PublishedAt: now.Add(-time.Hour)}},
	}
	for index := 0; index < 100; index++ {
		input.ExploreEvents = append(input.ExploreEvents, ExploreEventSignalInput{Topic: "photography", EventType: ExploreEventCompletedRead, OccurredAt: now.Add(-time.Duration(index) * time.Minute)})
	}
	profile := BuildExploreProfile(input)
	backend := rankFixture(1, "backend", []string{"backend"}, "provider-a", now)
	photography := rankFixture(2, "photography", nil, "provider-b", now)
	if got := rankedSourceIDs(RankExploreCandidates(profile, []RankCandidate{photography, backend}, now)); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("recent formal metadata did not outweigh explore behavior: %v", got)
	}
}

func TestExploreRankHardFiltersAndExistingSubscriptions(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{
		Now:           now,
		Subscriptions: []SubscriptionSignalInput{{SourceID: 1, Domain: "subscribed.example", Category: "tech"}},
		Feedback:      []ExplicitFeedbackInput{{SourceID: 2, Type: FeedbackHideSource}},
	})
	merged := 99
	candidates := []RankCandidate{
		rankFixture(1, "tech", nil, "provider", now),
		rankFixture(2, "tech", nil, "provider", now),
		func() RankCandidate {
			candidate := rankFixture(3, "tech", nil, "provider", now)
			candidate.Domain = "subscribed.example"
			return candidate
		}(),
		func() RankCandidate {
			candidate := rankFixture(4, "tech", nil, "provider", now)
			candidate.ValidationStatus = "invalid"
			return candidate
		}(),
		func() RankCandidate {
			candidate := rankFixture(5, "tech", nil, "provider", now)
			candidate.IsBroken = true
			return candidate
		}(),
		func() RankCandidate {
			candidate := rankFixture(6, "tech", nil, "provider", now)
			candidate.MergedIntoSourceID = &merged
			return candidate
		}(),
		func() RankCandidate {
			candidate := rankFixture(7, "tech", nil, "provider", now)
			candidate.Articles = nil
			return candidate
		}(),
		rankFixture(8, "tech", nil, "provider", now),
	}
	if got := rankedSourceIDs(RankExploreCandidates(profile, candidates, now)); !reflect.DeepEqual(got, []int{8}) {
		t.Fatalf("hard filters left %v", got)
	}
}

func TestExploreRankHealthThenFreshnessBreakTies(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{Now: now, Subscriptions: []SubscriptionSignalInput{{Category: "tech"}}})
	olderHealthy := rankFixture(1, "tech", nil, "provider", now.Add(-48*time.Hour))
	olderHealthy.HealthScore = 0.9
	fresherHealthy := rankFixture(2, "tech", nil, "provider", now.Add(-time.Hour))
	fresherHealthy.HealthScore = 0.9
	unhealthyFresh := rankFixture(3, "tech", nil, "provider", now)
	unhealthyFresh.HealthScore = 0.4
	if got := rankedSourceIDs(RankExploreCandidates(profile, []RankCandidate{unhealthyFresh, olderHealthy, fresherHealthy}, now)); !reflect.DeepEqual(got, []int{2, 1, 3}) {
		t.Fatalf("tie order=%v", got)
	}
}

func TestExploreRankAdjacentTopicCoverageGainChangesSelection(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{Now: now, Subscriptions: []SubscriptionSignalInput{{Tags: []string{"backend"}}}})
	firstGo := rankFixture(1, "go", []string{"backend"}, "provider", now)
	secondGo := rankFixture(2, "go", []string{"backend"}, "provider", now)
	rust := rankFixture(3, "rust", []string{"backend"}, "provider", now)

	if got := rankedSourceIDs(RankExploreCandidates(profile, []RankCandidate{secondGo, rust, firstGo}, now)); !reflect.DeepEqual(got, []int{1, 3, 2}) {
		t.Fatalf("coverage gain did not diversify adjacent topic: %v", got)
	}
}

func TestExploreRankColdStartLimitsAndDeterminism(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	candidates := make([]RankCandidate, 20)
	for index := range candidates {
		candidate := rankFixture(index+1, "topic", nil, "provider-z", now.Add(-time.Duration(index)*time.Hour))
		candidate.HealthScore = 1 - float64(index)/100
		candidate.Articles = make([]RankArticle, 7)
		for articleIndex := range candidate.Articles {
			candidate.Articles[articleIndex] = RankArticle{ID: (index+1)*100 + articleIndex, Title: "article", PublishedAt: now.Add(-time.Duration(articleIndex) * time.Hour)}
		}
		candidates[index] = candidate
	}
	profile := BuildExploreProfile(ProfileInput{Now: now})
	baseline := RankExploreCandidates(profile, candidates, now)
	if len(baseline) != MaxRankedSources {
		t.Fatalf("sources=%d", len(baseline))
	}
	for _, source := range baseline {
		if len(source.Articles) != MaxRankedArticlesPerSource || source.Reason != "来自持续更新的 provider-z 目录" || math.IsNaN(source.Score) || math.IsInf(source.Score, 0) {
			t.Fatalf("cold source=%+v", source)
		}
	}
	for seed := int64(1); seed <= 5; seed++ {
		shuffled := append([]RankCandidate(nil), candidates...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		for index := range shuffled {
			rand.New(rand.NewSource(seed+int64(index)+100)).Shuffle(len(shuffled[index].Articles), func(i, j int) {
				shuffled[index].Articles[i], shuffled[index].Articles[j] = shuffled[index].Articles[j], shuffled[index].Articles[i]
			})
		}
		if got := RankExploreCandidates(profile, shuffled, now); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("seed %d changed deterministic rank\nbase=%+v\ngot=%+v", seed, baseline, got)
		}
	}
}

func TestExploreRankSanitizesNonFiniteAndFutureMetadata(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	candidate := rankFixture(1, "topic", nil, "provider", now.Add(365*24*time.Hour))
	candidate.HealthScore = math.Inf(1)
	ranked := RankExploreCandidates(BuildExploreProfile(ProfileInput{Now: now}), []RankCandidate{candidate}, now)
	if len(ranked) != 1 || math.IsNaN(ranked[0].Score) || math.IsInf(ranked[0].Score, 0) {
		t.Fatalf("ranked=%+v", ranked)
	}
}

func TestExploreRankDeduplicatesSourceIDDeterministically(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{Now: now, Subscriptions: []SubscriptionSignalInput{{Tags: []string{"backend"}}}})
	weak := rankFixture(1, "unrelated", nil, "provider-z", now)
	best := rankFixture(1, "go", []string{"backend"}, "provider-a", now)
	other := rankFixture(2, "go", []string{"backend"}, "provider-b", now)
	forward := RankExploreCandidates(profile, []RankCandidate{weak, other, best}, now)
	reverse := RankExploreCandidates(profile, []RankCandidate{best, other, weak}, now)
	if !reflect.DeepEqual(forward, reverse) || !reflect.DeepEqual(rankedSourceIDs(forward), []int{1, 2}) || forward[0].Reason != "与你订阅的 backend 相关" {
		t.Fatalf("duplicate source was not deterministic\nforward=%+v\nreverse=%+v", forward, reverse)
	}
}

func TestExploreRankIgnoresStaleAndFutureObservations(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{Now: now, Subscriptions: []SubscriptionSignalInput{{Tags: []string{"backend"}}}})
	stale := rankFixture(1, "unrelated", nil, "provider", now)
	stale.Observations = []RankObservation{{Tags: []string{"backend"}, LastObservedAt: now.Add(-31 * 24 * time.Hour)}}
	future := rankFixture(2, "unrelated", nil, "provider", now)
	future.Observations = []RankObservation{{Tags: []string{"backend"}, LastObservedAt: now.Add(24 * time.Hour)}}
	recent := rankFixture(3, "unrelated", nil, "provider", now)
	recent.Observations = []RankObservation{{Tags: []string{"backend"}, LastObservedAt: now.Add(-time.Hour)}}
	if got := rankedSourceIDs(RankExploreCandidates(profile, []RankCandidate{stale, recent, future}, now)); !reflect.DeepEqual(got, []int{3, 1, 2}) {
		t.Fatalf("observation window order=%v", got)
	}
}

func rankFixture(id int, topic string, tags []string, provider string, publishedAt time.Time) RankCandidate {
	return RankCandidate{
		SourceID:         id,
		Title:            topic,
		Category:         topic,
		Topic:            topic,
		Tags:             tags,
		Domain:           "candidate.example",
		ValidationStatus: "valid",
		HealthScore:      0.8,
		Provider:         provider,
		Articles:         []RankArticle{{ID: id * 10, Title: topic + " article", PublishedAt: publishedAt}},
	}
}

func rankedSourceIDs(ranked []RankedSource) []int {
	ids := make([]int, len(ranked))
	for index := range ranked {
		ids[index] = ranked[index].SourceID
	}
	return ids
}

func rankedScores(ranked []RankedSource) []float64 {
	scores := make([]float64, len(ranked))
	for index := range ranked {
		scores[index] = ranked[index].Score
	}
	return scores
}
