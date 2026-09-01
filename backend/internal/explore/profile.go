package explore

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	MaxProfileSubscriptionSignals = 128
	MaxProfileBehaviorSignals     = 64
	MaxProfileContentRunes        = 4000
	MaxProfileSnippetRunes        = 1000
	maxProfileFormalBehaviors     = 100

	ExploreEventExposure      = "exposure"
	ExploreEventClick         = "click"
	ExploreEventCompletedRead = "completed_read"
	FormalArticleRead         = "read"
	FormalArticleSave         = "save"
	FormalArticleLike         = "like"

	FeedbackHideSource  = "hide_source"
	FeedbackDampenTopic = "dampen_topic"
	FeedbackBoostTopic  = "boost_topic"
)

type SignalKind string

const (
	SignalToken    SignalKind = "token"
	SignalCategory SignalKind = "category"
	SignalDomain   SignalKind = "domain"
	SignalTag      SignalKind = "tag"
	SignalTopic    SignalKind = "topic"
)

type WeightedSignal struct {
	Kind   SignalKind
	Value  string
	Weight float64
}

// ProfileInput contains personalized metadata but no user identity, article
// body, or private URL. Repository code owns access control and projection.
type ProfileInput struct {
	Now                    time.Time
	Subscriptions          []SubscriptionSignalInput
	RecentArticles         []RecentArticleSignalInput
	FormalArticleBehaviors []FormalArticleBehaviorInput
	ExploreEvents          []ExploreEventSignalInput
	Feedback               []ExplicitFeedbackInput
}

type SubscriptionSignalInput struct {
	SourceID int
	Title    string
	Category string
	Domain   string
	Tags     []string
}

type RecentArticleSignalInput struct {
	Title       string
	Category    string
	Topic       string
	Tags        []string
	TextTokens  []string
	PublishedAt time.Time
}

type FormalArticleBehaviorInput struct {
	Title      string
	Category   string
	Topic      string
	Tags       []string
	SignalType string
	OccurredAt time.Time
}

type ExploreEventSignalInput struct {
	SourceID   int
	Topic      string
	EventType  string
	OccurredAt time.Time
}

type ExplicitFeedbackInput struct {
	SourceID int
	Topic    string
	Type     string
}

// ExploreProfile is deterministic, bounded, and safe to pass only inside the
// personalized ranking path. Sorted slices avoid map-order leakage.
type ExploreProfile struct {
	SubscriptionSignals []WeightedSignal
	BehaviorSignals     []WeightedSignal
	VisibleSourceIDs    []int
	VisibleDomains      []string
	HiddenSourceIDs     []int
	BoostTopics         []string
	DampenTopics        []string
}

type signalAccumulator struct {
	values map[string]WeightedSignal
}

func BuildExploreProfile(input ProfileInput) ExploreProfile {
	subscriptions := signalAccumulator{values: make(map[string]WeightedSignal)}
	behavior := signalAccumulator{values: make(map[string]WeightedSignal)}
	visibleSources := make(map[int]struct{})
	visibleDomains := make(map[string]struct{})

	for _, subscription := range input.Subscriptions {
		if subscription.SourceID > 0 {
			visibleSources[subscription.SourceID] = struct{}{}
		}
		for _, token := range normalizeSignalTokens(subscription.Title, 8) {
			subscriptions.addMax(SignalToken, token, 1)
		}
		if category := normalizeSignalPhrase(subscription.Category); category != "" {
			subscriptions.addMax(SignalCategory, category, 2)
		}
		if domain := normalizeSignalDomain(subscription.Domain); domain != "" {
			visibleDomains[domain] = struct{}{}
			subscriptions.addMax(SignalDomain, domain, 2)
		}
		for _, tag := range normalizeSignalList(subscription.Tags, 20) {
			subscriptions.addMax(SignalTag, tag, 2.5)
		}
	}

	recentArticles := append([]RecentArticleSignalInput(nil), input.RecentArticles...)
	recentArticles = filterRecentArticles(recentArticles, input.Now)
	sort.Slice(recentArticles, func(i, j int) bool {
		if !recentArticles[i].PublishedAt.Equal(recentArticles[j].PublishedAt) {
			return recentArticles[i].PublishedAt.After(recentArticles[j].PublishedAt)
		}
		return recentArticles[i].Title < recentArticles[j].Title
	})
	if len(recentArticles) > 50 {
		recentArticles = recentArticles[:50]
	}
	for _, article := range recentArticles {
		for _, token := range normalizeSignalTokens(article.Title, 8) {
			subscriptions.addMax(SignalToken, token, 0.6)
		}
		if category := normalizeSignalPhrase(article.Category); category != "" {
			subscriptions.addMax(SignalCategory, category, 1.2)
		}
		if topic := normalizeSignalPhrase(article.Topic); topic != "" {
			subscriptions.addMax(SignalTopic, topic, 1.2)
			for _, token := range normalizeSignalTokens(topic, 8) {
				subscriptions.addMax(SignalToken, token, 0.8)
			}
		}
		for _, tag := range normalizeSignalList(article.Tags, 20) {
			subscriptions.addMax(SignalTag, tag, 1.5)
			subscriptions.addMax(SignalToken, tag, 0.8)
		}
		for _, token := range normalizeSignalList(article.TextTokens, 24) {
			subscriptions.addMax(SignalToken, token, 0.5)
		}
	}

	formalBehaviors := boundedFormalArticleBehaviors(input.FormalArticleBehaviors, input.Now)
	for _, interaction := range formalBehaviors {
		weight := map[string]float64{
			FormalArticleRead: 0.02,
			FormalArticleSave: 0.05,
			FormalArticleLike: 0.08,
		}[interaction.SignalType]
		if weight == 0 {
			continue
		}
		for _, token := range normalizeSignalTokens(interaction.Title, 8) {
			behavior.addCapped(SignalToken, token, weight*0.5, 0.25)
		}
		if category := normalizeSignalPhrase(interaction.Category); category != "" {
			behavior.addCapped(SignalCategory, category, weight*0.8, 0.4)
		}
		if topic := normalizeSignalPhrase(interaction.Topic); topic != "" {
			behavior.addCapped(SignalTopic, topic, weight, 0.5)
		}
		for _, tag := range normalizeSignalList(interaction.Tags, 20) {
			behavior.addCapped(SignalTag, tag, weight*0.8, 0.4)
		}
	}

	events := append([]ExploreEventSignalInput(nil), input.ExploreEvents...)
	events = filterRecentExploreEvents(events, input.Now)
	sort.Slice(events, func(i, j int) bool {
		if !events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].OccurredAt.After(events[j].OccurredAt)
		}
		if events[i].Topic != events[j].Topic {
			return events[i].Topic < events[j].Topic
		}
		return events[i].EventType < events[j].EventType
	})
	if len(events) > 100 {
		events = events[:100]
	}
	for _, event := range events {
		weight := map[string]float64{
			ExploreEventExposure:      0.02,
			ExploreEventClick:         0.06,
			ExploreEventCompletedRead: 0.1,
		}[event.EventType]
		if topic := normalizeSignalPhrase(event.Topic); topic != "" && weight > 0 {
			behavior.addCapped(SignalTopic, topic, weight, 0.5)
		}
	}

	hiddenSources := make(map[int]struct{})
	boostTopics := make(map[string]struct{})
	dampenTopics := make(map[string]struct{})
	for _, feedback := range input.Feedback {
		switch feedback.Type {
		case FeedbackHideSource:
			if feedback.SourceID > 0 {
				hiddenSources[feedback.SourceID] = struct{}{}
			}
		case FeedbackBoostTopic:
			if topic := normalizeSignalPhrase(feedback.Topic); topic != "" {
				boostTopics[topic] = struct{}{}
			}
		case FeedbackDampenTopic:
			if topic := normalizeSignalPhrase(feedback.Topic); topic != "" {
				dampenTopics[topic] = struct{}{}
			}
		}
	}

	return ExploreProfile{
		SubscriptionSignals: subscriptions.sorted(MaxProfileSubscriptionSignals),
		BehaviorSignals:     behavior.sorted(MaxProfileBehaviorSignals),
		VisibleSourceIDs:    sortedPositiveInts(visibleSources),
		VisibleDomains:      sortedStringSet(visibleDomains),
		HiddenSourceIDs:     sortedPositiveInts(hiddenSources),
		BoostTopics:         sortedStringSet(boostTopics),
		DampenTopics:        sortedStringSet(dampenTopics),
	}
}

// ClipProfileText keeps SQL/profile inputs bounded before tokenization. It is
// rune-aware so truncation cannot manufacture invalid UTF-8.
func ClipProfileText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

// ProfileTextTokens extracts a bounded projection and drops the raw formal
// article body before it crosses into the ranking profile.
func ProfileTextTokens(content, snippet string) []string {
	combined := ClipProfileText(content, MaxProfileContentRunes) + " " + ClipProfileText(snippet, MaxProfileSnippetRunes)
	return normalizeSignalTokens(combined, 24)
}

// FreshObservationWindow is the recommendation evidence window for one
// provider: two expected sync periods, with enough slack for short intervals.
func FreshObservationWindow(syncIntervalMinutes int) time.Duration {
	window := time.Duration(syncIntervalMinutes) * 2 * time.Minute
	if window < 6*time.Hour {
		return 6 * time.Hour
	}
	return window
}

func boundedFormalArticleBehaviors(values []FormalArticleBehaviorInput, now time.Time) []FormalArticleBehaviorInput {
	result := make([]FormalArticleBehaviorInput, 0, maxProfileFormalBehaviors)
	for _, value := range values {
		if !withinProfileWindow(value.OccurredAt, now, 30*24*time.Hour) || !knownFormalArticleBehavior(value.SignalType) {
			continue
		}
		if len(result) < maxProfileFormalBehaviors {
			result = append(result, value)
			continue
		}
		worst := 0
		for index := 1; index < len(result); index++ {
			if formalArticleBehaviorLess(result[worst], result[index]) {
				worst = index
			}
		}
		if formalArticleBehaviorLess(value, result[worst]) {
			result[worst] = value
		}
	}
	sort.Slice(result, func(i, j int) bool { return formalArticleBehaviorLess(result[i], result[j]) })
	return result
}

func formalArticleBehaviorLess(left, right FormalArticleBehaviorInput) bool {
	if !left.OccurredAt.Equal(right.OccurredAt) {
		return left.OccurredAt.After(right.OccurredAt)
	}
	return formalArticleBehaviorKey(left) < formalArticleBehaviorKey(right)
}

func knownFormalArticleBehavior(value string) bool {
	return value == FormalArticleRead || value == FormalArticleSave || value == FormalArticleLike
}

func formalArticleBehaviorKey(value FormalArticleBehaviorInput) string {
	return strings.Join([]string{
		value.SignalType,
		normalizeSignalPhrase(value.Title),
		normalizeSignalPhrase(value.Category),
		normalizeSignalPhrase(value.Topic),
		strings.Join(normalizeSignalList(value.Tags, 20), ","),
	}, "\x00")
}

func filterRecentArticles(values []RecentArticleSignalInput, now time.Time) []RecentArticleSignalInput {
	result := values[:0]
	for _, value := range values {
		if withinProfileWindow(value.PublishedAt, now, 30*24*time.Hour) {
			result = append(result, value)
		}
	}
	return result
}

func filterRecentExploreEvents(values []ExploreEventSignalInput, now time.Time) []ExploreEventSignalInput {
	result := values[:0]
	for _, value := range values {
		if withinProfileWindow(value.OccurredAt, now, 30*24*time.Hour) {
			result = append(result, value)
		}
	}
	return result
}

func (accumulator signalAccumulator) addMax(kind SignalKind, value string, weight float64) {
	key := string(kind) + "\x00" + value
	if current, exists := accumulator.values[key]; !exists || weight > current.Weight {
		accumulator.values[key] = WeightedSignal{Kind: kind, Value: value, Weight: weight}
	}
}

func (accumulator signalAccumulator) addCapped(kind SignalKind, value string, delta, capValue float64) {
	key := string(kind) + "\x00" + value
	current := accumulator.values[key]
	current.Kind, current.Value = kind, value
	current.Weight += delta
	if current.Weight > capValue {
		current.Weight = capValue
	}
	accumulator.values[key] = current
}

func (accumulator signalAccumulator) sorted(limit int) []WeightedSignal {
	values := make([]WeightedSignal, 0, len(accumulator.values))
	for _, signal := range accumulator.values {
		values = append(values, signal)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Weight != values[j].Weight {
			return values[i].Weight > values[j].Weight
		}
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].Value < values[j].Value
	})
	if len(values) > limit {
		values = values[:limit]
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].Value < values[j].Value
	})
	return values
}

func withinProfileWindow(value, now time.Time, window time.Duration) bool {
	if value.IsZero() || now.IsZero() || value.After(now.Add(5*time.Minute)) {
		return false
	}
	return !value.Before(now.Add(-window))
}

func normalizeSignalTokens(value string, limit int) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return normalizeSignalList(parts, limit)
}

func normalizeSignalList(values []string, limit int) []string {
	set := make(map[string]struct{})
	for _, value := range values {
		normalized := normalizeSignalPhrase(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	result := sortedStringSet(set)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func normalizeSignalPhrase(value string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return ""
	}
	result := strings.Join(parts, " ")
	runes := []rune(result)
	if len(runes) > 64 {
		result = string(runes[:64])
	}
	return result
}

func normalizeSignalDomain(value string) string {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	domain = strings.TrimPrefix(domain, "www.")
	if len(domain) > 253 || strings.ContainsAny(domain, " /?#@") {
		return ""
	}
	return domain
}

func sortedPositiveInts(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		if value > 0 {
			result = append(result, value)
		}
	}
	sort.Ints(result)
	return result
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
