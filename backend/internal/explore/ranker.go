package explore

import (
	"encoding/binary"
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
		candidate.HealthScore = health
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
	bestByID := make(map[int]RankArticle, len(input))
	for _, article := range input {
		cleaned, ok := cleanRankArticle(article, now)
		if !ok {
			continue
		}
		current, exists := bestByID[cleaned.ID]
		if !exists || rankArticleLess(cleaned, current) {
			bestByID[cleaned.ID] = cleaned
		}
	}
	articles := make([]RankArticle, 0, len(bestByID))
	for _, article := range bestByID {
		articles = append(articles, article)
	}
	sort.Slice(articles, func(i, j int) bool {
		return rankArticleLess(articles[i], articles[j])
	})
	if len(articles) > MaxRankedArticlesPerSource {
		articles = articles[:MaxRankedArticlesPerSource]
	}
	freshness := time.Time{}
	if len(articles) > 0 {
		freshness = effectiveRankArticleTime(articles[0])
	}
	return articles, freshness
}

func cleanRankArticle(article RankArticle, now time.Time) (RankArticle, bool) {
	if article.ID <= 0 {
		return RankArticle{}, false
	}
	article.Tags = append([]string(nil), article.Tags...)
	article.PublishedAt = cleanRankArticleTime(article.PublishedAt, now)
	article.FetchedAt = cleanRankArticleTime(article.FetchedAt, now)
	if article.PublishedAt.IsZero() && article.FetchedAt.IsZero() {
		return RankArticle{}, false
	}
	return article, true
}

func cleanRankArticleTime(value, now time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	value = value.Round(0).UTC()
	if !now.IsZero() && value.After(now.Add(5*time.Minute)) {
		return time.Time{}
	}
	return value
}

func rankArticleLess(left, right RankArticle) bool {
	leftTime, rightTime := effectiveRankArticleTime(left), effectiveRankArticleTime(right)
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	return rawRankArticleFingerprint(left) < rawRankArticleFingerprint(right)
}

func effectiveRankArticleTime(article RankArticle) time.Time {
	if !article.PublishedAt.IsZero() {
		return article.PublishedAt
	}
	return article.FetchedAt
}

func boundedRankObservations(input []RankObservation, now time.Time) []RankObservation {
	observations := make([]RankObservation, 0, len(input))
	for _, observation := range input {
		if withinProfileWindow(observation.LastObservedAt, now, 30*24*time.Hour) {
			observation.LastObservedAt = observation.LastObservedAt.Round(0)
			observation.Tags = append([]string(nil), observation.Tags...)
			observations = append(observations, observation)
		}
	}
	sort.Slice(observations, func(i, j int) bool {
		if !observations[i].LastObservedAt.Equal(observations[j].LastObservedAt) {
			return observations[i].LastObservedAt.After(observations[j].LastObservedAt)
		}
		left := normalizeProviderLabel(observations[i].Provider) + "\x00" + normalizeSignalPhrase(observations[i].Topic) + "\x00" + strings.Join(normalizeSignalList(observations[i].Tags, 20), ",")
		right := normalizeProviderLabel(observations[j].Provider) + "\x00" + normalizeSignalPhrase(observations[j].Topic) + "\x00" + strings.Join(normalizeSignalList(observations[j].Tags, 20), ",")
		if left != right {
			return left < right
		}
		return rawRankObservationFingerprint(observations[i]) < rawRankObservationFingerprint(observations[j])
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
	leftNormalized, rightNormalized := stableRankCandidateKey(left), stableRankCandidateKey(right)
	if leftNormalized != rightNormalized {
		return leftNormalized < rightNormalized
	}
	return rawRankCandidateFingerprint(left.candidate) < rawRankCandidateFingerprint(right.candidate)
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

func rawRankCandidateFingerprint(candidate RankCandidate) string {
	fingerprint := make([]byte, 0, 256)
	fingerprint = appendRankInt(fingerprint, candidate.SourceID)
	fingerprint = appendRankString(fingerprint, candidate.Title)
	fingerprint = appendRankString(fingerprint, candidate.Category)
	fingerprint = appendRankString(fingerprint, candidate.Domain)
	fingerprint = appendRankString(fingerprint, candidate.Topic)
	fingerprint = appendRankStrings(fingerprint, candidate.Tags)
	fingerprint = appendRankString(fingerprint, candidate.ValidationStatus)
	fingerprint = appendRankBool(fingerprint, candidate.IsBroken)
	fingerprint = appendRankOptionalInt(fingerprint, candidate.MergedIntoSourceID)
	fingerprint = appendRankUint64(fingerprint, math.Float64bits(candidate.HealthScore))
	fingerprint = appendRankString(fingerprint, candidate.Provider)
	fingerprint = appendRankUint64(fingerprint, uint64(len(candidate.Observations)))
	for _, observation := range candidate.Observations {
		fingerprint = appendRankBytes(fingerprint, []byte(rawRankObservationFingerprint(observation)))
	}
	fingerprint = appendRankUint64(fingerprint, uint64(len(candidate.Articles)))
	for _, article := range candidate.Articles {
		fingerprint = appendRankBytes(fingerprint, []byte(rawRankArticleFingerprint(article)))
	}
	return string(fingerprint)
}

func rawRankObservationFingerprint(observation RankObservation) string {
	fingerprint := make([]byte, 0, 96)
	fingerprint = appendRankString(fingerprint, observation.Provider)
	fingerprint = appendRankString(fingerprint, observation.Topic)
	fingerprint = appendRankStrings(fingerprint, observation.Tags)
	fingerprint = appendRankTime(fingerprint, observation.LastObservedAt)
	return string(fingerprint)
}

func rawRankArticleFingerprint(article RankArticle) string {
	fingerprint := make([]byte, 0, 128)
	fingerprint = appendRankInt(fingerprint, article.ID)
	fingerprint = appendRankString(fingerprint, article.Title)
	fingerprint = appendRankString(fingerprint, article.Topic)
	fingerprint = appendRankStrings(fingerprint, article.Tags)
	fingerprint = appendRankTime(fingerprint, article.PublishedAt)
	fingerprint = appendRankTime(fingerprint, article.FetchedAt)
	return string(fingerprint)
}

func appendRankString(fingerprint []byte, value string) []byte {
	return appendRankBytes(fingerprint, []byte(value))
}

func appendRankStrings(fingerprint []byte, values []string) []byte {
	fingerprint = appendRankUint64(fingerprint, uint64(len(values)))
	for _, value := range values {
		fingerprint = appendRankString(fingerprint, value)
	}
	return fingerprint
}

func appendRankBytes(fingerprint, value []byte) []byte {
	fingerprint = appendRankUint64(fingerprint, uint64(len(value)))
	return append(fingerprint, value...)
}

func appendRankInt(fingerprint []byte, value int) []byte {
	return appendRankUint64(fingerprint, uint64(int64(value)))
}

func appendRankOptionalInt(fingerprint []byte, value *int) []byte {
	if value == nil {
		return appendRankBool(fingerprint, false)
	}
	fingerprint = appendRankBool(fingerprint, true)
	return appendRankInt(fingerprint, *value)
}

func appendRankBool(fingerprint []byte, value bool) []byte {
	if value {
		return append(fingerprint, 1)
	}
	return append(fingerprint, 0)
}

func appendRankTime(fingerprint []byte, value time.Time) []byte {
	if value.IsZero() {
		return appendRankBool(fingerprint, false)
	}
	fingerprint = appendRankBool(fingerprint, true)
	value = value.Round(0)
	fingerprint = appendRankString(fingerprint, value.Format(time.RFC3339Nano))
	fingerprint = appendRankString(fingerprint, value.Location().String())
	zoneName, zoneOffset := value.Zone()
	fingerprint = appendRankString(fingerprint, zoneName)
	return appendRankInt(fingerprint, zoneOffset)
}

func appendRankUint64(fingerprint []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(fingerprint, encoded[:]...)
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
