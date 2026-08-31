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

	ExploreEventExposure      = "exposure"
	ExploreEventClick         = "click"
	ExploreEventCompletedRead = "completed_read"

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
	Now            time.Time
	Subscriptions  []SubscriptionSignalInput
	RecentArticles []RecentArticleSignalInput
	ExploreEvents  []ExploreEventSignalInput
	Feedback       []ExplicitFeedbackInput
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
	Tags        []string
	PublishedAt time.Time
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
			behavior.addCapped(SignalToken, token, 0.08, 0.5)
		}
		if category := normalizeSignalPhrase(article.Category); category != "" {
			behavior.addCapped(SignalCategory, category, 0.15, 0.75)
		}
		for _, tag := range normalizeSignalList(article.Tags, 20) {
			behavior.addCapped(SignalTag, tag, 0.12, 0.6)
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
