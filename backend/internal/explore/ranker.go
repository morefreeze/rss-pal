package explore

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxRankedSources             = 12
	MaxRankedArticlesPerSource   = 5
	maxRankCandidateObservations = 50
)

type RankCandidate struct {
	SourceID           int
	Title              string
	Category           string
	Domain             string
	Topic              string
	Tags               []string
	ValidationStatus   string
	IsBroken           bool
	MergedIntoSourceID *int
	HealthScore        float64
	Provider           string
	Observations       []RankObservation
	Articles           []RankArticle
}

type RankObservation struct {
	Provider       string
	Topic          string
	Tags           []string
	LastObservedAt time.Time
}

type RankArticle struct {
	ID          int
	Title       string
	Topic       string
	Tags        []string
	PublishedAt time.Time
	FetchedAt   time.Time
}

type RankedSource struct {
	SourceID int
	Score    float64
	Topic    string
	Reason   string
	Articles []RankArticle
}

type scoredRankCandidate struct {
	candidate         RankCandidate
	articles          []RankArticle
	subscriptionScore float64
	behaviorScore     float64
	feedbackScore     float64
	health            float64
	freshness         time.Time
	primaryTopic      string
	reasonSignal      string
	provider          string
	score             float64
}

// RankExploreCandidates is pure and deterministic. It filters before scoring,
// applies bounded behavior below subscription similarity, then applies active
// explicit feedback after all implicit signals.
func RankExploreCandidates(profile ExploreProfile, candidates []RankCandidate, now time.Time) []RankedSource {
	subscriptionWeights := signalWeights(profile.SubscriptionSignals)
	behaviorWeights := signalWeights(profile.BehaviorSignals)
	visibleSources := intSliceSet(profile.VisibleSourceIDs)
	visibleDomains := stringSliceSet(profile.VisibleDomains)
	hiddenSources := intSliceSet(profile.HiddenSourceIDs)
	boostTopics := stringSliceSet(profile.BoostTopics)
	dampenTopics := stringSliceSet(profile.DampenTopics)
	coldStart := len(subscriptionWeights) == 0

	bestBySource := make(map[int]scoredRankCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.SourceID <= 0 || candidate.ValidationStatus != "valid" || candidate.IsBroken || candidate.MergedIntoSourceID != nil || len(candidate.Articles) == 0 {
			continue
		}
		domain := normalizeSignalDomain(candidate.Domain)
		if _, excluded := visibleSources[candidate.SourceID]; excluded {
			continue
		}
		if _, excluded := hiddenSources[candidate.SourceID]; excluded {
			continue
		}
		if domain != "" {
			if _, excluded := visibleDomains[domain]; excluded {
				continue
			}
		}

		articles, freshness := rankedArticles(candidate.Articles, now)
		if len(articles) == 0 {
			continue
		}
		candidate.Articles = articles
		candidate.Observations = boundedRankObservations(candidate.Observations, now)
		signals := candidateSignals(candidate)
		subscriptionScore, reasonSignal := matchCandidateSignals(signals, subscriptionWeights)
		behaviorScore, _ := matchCandidateSignals(signals, behaviorWeights)
		if behaviorScore > 1 {
			behaviorScore = 1
		}
		topics := candidateTopics(candidate)
		feedbackScore := 0.0
		for topic := range topics {
			if _, boosted := boostTopics[topic]; boosted {
				feedbackScore += 4
			}
			if _, dampened := dampenTopics[topic]; dampened {
				feedbackScore -= 4
			}
		}
		if feedbackScore > 4 {
			feedbackScore = 4
		}
		if feedbackScore < -4 {
			feedbackScore = -4
		}
		health := finiteUnit(candidate.HealthScore)
		primaryTopic := normalizeSignalPhrase(candidate.Topic)
		if primaryTopic == "" {
			primaryTopic = normalizeSignalPhrase(candidate.Category)
		}
		provider := canonicalCandidateProvider(candidate)
		scoredCandidate := scoredRankCandidate{
			candidate:         candidate,
			articles:          articles,
			subscriptionScore: subscriptionScore,
			behaviorScore:     behaviorScore,
			feedbackScore:     feedbackScore,
			health:            health,
			freshness:         freshness,
			primaryTopic:      primaryTopic,
			reasonSignal:      reasonSignal,
			provider:          provider,
			score:             subscriptionScore*10 + behaviorScore + feedbackScore,
		}
		current, exists := bestBySource[candidate.SourceID]
		if !exists || rankCandidateVariantLess(scoredCandidate, current) {
			bestBySource[candidate.SourceID] = scoredCandidate
		}
	}

	scored := make([]scoredRankCandidate, 0, len(bestBySource))
	for _, candidate := range bestBySource {
		scored = append(scored, candidate)
	}

	selected := make([]RankedSource, 0, minInt(MaxRankedSources, len(scored)))
	coveredTopics := make(map[string]struct{})
	for len(scored) > 0 && len(selected) < MaxRankedSources {
		bestIndex := 0
		bestScore := selectionScore(scored[0], coveredTopics, coldStart)
		for index := 1; index < len(scored); index++ {
			score := selectionScore(scored[index], coveredTopics, coldStart)
			if rankCandidateLess(scored[index], score, scored[bestIndex], bestScore) {
				bestIndex, bestScore = index, score
			}
		}
		best := scored[bestIndex]
		reason := fmt.Sprintf("来自持续更新的 %s 目录", best.provider)
		if best.reasonSignal != "" {
			reason = fmt.Sprintf("与你订阅的 %s 相关", best.reasonSignal)
		}
		selected = append(selected, RankedSource{
			SourceID: best.candidate.SourceID,
			Score:    finite(bestScore),
			Topic:    best.primaryTopic,
			Reason:   reason,
			Articles: best.articles,
		})
		if best.primaryTopic != "" {
			coveredTopics[best.primaryTopic] = struct{}{}
		}
		scored = append(scored[:bestIndex], scored[bestIndex+1:]...)
	}
	return selected
}

func signalWeights(signals []WeightedSignal) map[string]WeightedSignal {
	weights := make(map[string]WeightedSignal, len(signals))
	for _, signal := range signals {
		if signal.Value == "" || signal.Weight <= 0 || math.IsNaN(signal.Weight) || math.IsInf(signal.Weight, 0) {
			continue
		}
		key := string(signal.Kind) + "\x00" + signal.Value
		if current, exists := weights[key]; !exists || signal.Weight > current.Weight {
			weights[key] = signal
		}
	}
	return weights
}

func candidateSignals(candidate RankCandidate) map[string]WeightedSignal {
	result := make(map[string]WeightedSignal)
	add := func(kind SignalKind, value string) {
		if value != "" {
			result[string(kind)+"\x00"+value] = WeightedSignal{Kind: kind, Value: value, Weight: 1}
		}
	}
	for _, token := range normalizeSignalTokens(candidate.Title, 8) {
		add(SignalToken, token)
	}
	add(SignalCategory, normalizeSignalPhrase(candidate.Category))
	add(SignalDomain, normalizeSignalDomain(candidate.Domain))
	add(SignalTopic, normalizeSignalPhrase(candidate.Topic))
	for _, tag := range normalizeSignalList(candidate.Tags, 20) {
		add(SignalTag, tag)
	}
	for _, observation := range candidate.Observations {
		add(SignalTopic, normalizeSignalPhrase(observation.Topic))
		for _, tag := range normalizeSignalList(observation.Tags, 20) {
			add(SignalTag, tag)
		}
	}
	for _, article := range candidate.Articles {
		for _, token := range normalizeSignalTokens(article.Title, 4) {
			add(SignalToken, token)
		}
		add(SignalTopic, normalizeSignalPhrase(article.Topic))
		for _, tag := range normalizeSignalList(article.Tags, 10) {
			add(SignalTag, tag)
		}
	}
	return result
}

func matchCandidateSignals(candidate map[string]WeightedSignal, weights map[string]WeightedSignal) (float64, string) {
	matched := make([]WeightedSignal, 0)
	for key := range candidate {
		if signal, exists := weights[key]; exists {
			matched = append(matched, signal)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Weight != matched[j].Weight {
			return matched[i].Weight > matched[j].Weight
		}
		if matched[i].Kind != matched[j].Kind {
			return matched[i].Kind < matched[j].Kind
		}
		return matched[i].Value < matched[j].Value
	})
	score := 0.0
	for _, signal := range matched {
		score += signal.Weight
	}
	if len(matched) == 0 {
		return 0, ""
	}
	return finite(score), matched[0].Value
}

func candidateTopics(candidate RankCandidate) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range append(append([]string{candidate.Topic, candidate.Category}, candidate.Tags...), observationTopics(candidate.Observations)...) {
		if normalized := normalizeSignalPhrase(value); normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func observationTopics(observations []RankObservation) []string {
	result := make([]string, 0, len(observations))
	for _, observation := range observations {
		result = append(result, observation.Topic)
		result = append(result, observation.Tags...)
	}
	return result
}

func rankedArticles(input []RankArticle, now time.Time) ([]RankArticle, time.Time) {
	articles := append([]RankArticle(nil), input...)
	sort.Slice(articles, func(i, j int) bool {
		left, right := safeArticleTime(articles[i], now), safeArticleTime(articles[j], now)
		if !left.Equal(right) {
			return left.After(right)
		}
		if articles[i].ID != articles[j].ID {
			return articles[i].ID < articles[j].ID
		}
		return stableRankArticleKey(articles[i]) < stableRankArticleKey(articles[j])
	})
	if len(articles) > MaxRankedArticlesPerSource {
		articles = articles[:MaxRankedArticlesPerSource]
	}
	freshness := time.Time{}
	if len(articles) > 0 {
		freshness = safeArticleTime(articles[0], now)
	}
	return articles, freshness
}

func boundedRankObservations(input []RankObservation, now time.Time) []RankObservation {
	observations := make([]RankObservation, 0, len(input))
	for _, observation := range input {
		if withinProfileWindow(observation.LastObservedAt, now, 30*24*time.Hour) {
			observations = append(observations, observation)
		}
	}
	sort.Slice(observations, func(i, j int) bool {
		if !observations[i].LastObservedAt.Equal(observations[j].LastObservedAt) {
			return observations[i].LastObservedAt.After(observations[j].LastObservedAt)
		}
		left := normalizeProviderLabel(observations[i].Provider) + "\x00" + normalizeSignalPhrase(observations[i].Topic) + "\x00" + strings.Join(normalizeSignalList(observations[i].Tags, 20), ",")
		right := normalizeProviderLabel(observations[j].Provider) + "\x00" + normalizeSignalPhrase(observations[j].Topic) + "\x00" + strings.Join(normalizeSignalList(observations[j].Tags, 20), ",")
		return left < right
	})
	if len(observations) > maxRankCandidateObservations {
		observations = observations[:maxRankCandidateObservations]
	}
	return observations
}

func stableRankArticleKey(article RankArticle) string {
	return strings.Join([]string{
		strconv.Itoa(article.ID),
		normalizeSignalPhrase(article.Title),
		normalizeSignalPhrase(article.Topic),
		strings.Join(normalizeSignalList(article.Tags, 20), ","),
		article.PublishedAt.UTC().Format(time.RFC3339Nano),
		article.FetchedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
}

func safeArticleTime(article RankArticle, now time.Time) time.Time {
	value := article.PublishedAt
	if value.IsZero() {
		value = article.FetchedAt
	}
	if !now.IsZero() && value.After(now.Add(5*time.Minute)) {
		return now
	}
	return value
}

func canonicalCandidateProvider(candidate RankCandidate) string {
	providers := make(map[string]struct{})
	if provider := normalizeProviderLabel(candidate.Provider); provider != "" {
		providers[provider] = struct{}{}
	}
	for _, observation := range candidate.Observations {
		if provider := normalizeProviderLabel(observation.Provider); provider != "" {
			providers[provider] = struct{}{}
		}
	}
	values := sortedStringSet(providers)
	if len(values) == 0 {
		return "公开来源"
	}
	return values[0]
}

func normalizeProviderLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 64 {
		value = string(runes[:64])
	}
	return value
}

func selectionScore(candidate scoredRankCandidate, covered map[string]struct{}, coldStart bool) float64 {
	score := candidate.score
	if !coldStart && candidate.subscriptionScore > 0 && candidate.primaryTopic != "" {
		if _, coveredAlready := covered[candidate.primaryTopic]; !coveredAlready {
			score += 0.25
		}
	}
	return finite(score)
}

func rankCandidateLess(left scoredRankCandidate, leftScore float64, right scoredRankCandidate, rightScore float64) bool {
	if leftScore != rightScore {
		return leftScore > rightScore
	}
	if left.health != right.health {
		return left.health > right.health
	}
	if !left.freshness.Equal(right.freshness) {
		return left.freshness.After(right.freshness)
	}
	return left.candidate.SourceID < right.candidate.SourceID
}

func rankCandidateVariantLess(left, right scoredRankCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.health != right.health {
		return left.health > right.health
	}
	if !left.freshness.Equal(right.freshness) {
		return left.freshness.After(right.freshness)
	}
	return stableRankCandidateKey(left) < stableRankCandidateKey(right)
}

func stableRankCandidateKey(candidate scoredRankCandidate) string {
	parts := []string{
		normalizeSignalPhrase(candidate.candidate.Title),
		normalizeSignalPhrase(candidate.candidate.Category),
		normalizeSignalDomain(candidate.candidate.Domain),
		normalizeSignalPhrase(candidate.candidate.Topic),
		strings.Join(normalizeSignalList(candidate.candidate.Tags, 20), ","),
		candidate.provider,
		candidate.reasonSignal,
	}
	articleParts := make([]string, 0, len(candidate.articles))
	for _, article := range candidate.articles {
		articleParts = append(articleParts, stableRankArticleKey(article))
	}
	parts = append(parts, strings.Join(articleParts, "\x1e"))
	observationParts := make([]string, 0, len(candidate.candidate.Observations))
	for _, observation := range candidate.candidate.Observations {
		observationParts = append(observationParts, normalizeProviderLabel(observation.Provider)+"\x1f"+normalizeSignalPhrase(observation.Topic)+"\x1f"+strings.Join(normalizeSignalList(observation.Tags, 20), ","))
	}
	sort.Strings(observationParts)
	parts = append(parts, strings.Join(observationParts, "\x1e"))
	return strings.Join(parts, "\x00")
}

func finiteUnit(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func finite(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func intSliceSet(values []int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func stringSliceSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
