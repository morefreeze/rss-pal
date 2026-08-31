package explore

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestExploreProfileNormalizesDeduplicatesAndBoundsSignals(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	profile := BuildExploreProfile(ProfileInput{
		Now: now,
		Subscriptions: []SubscriptionSignalInput{
			{SourceID: 7, Title: " Go, GO & Rust! ", Category: "Programming", Domain: "WWW.Example.COM.", Tags: []string{"Backend", " backend ", ""}},
			{SourceID: 7, Title: "Go", Category: "programming", Domain: "example.com", Tags: []string{"backend"}},
		},
		RecentArticles: []RecentArticleSignalInput{
			{Title: "Go runtimes", Category: "Programming", Tags: []string{"backend"}, PublishedAt: now.Add(-29 * 24 * time.Hour)},
			{Title: "expired", Category: "Ignored", PublishedAt: now.Add(-31 * 24 * time.Hour)},
			{Title: "future", Category: "Ignored", PublishedAt: now.Add(24 * time.Hour)},
		},
		ExploreEvents: []ExploreEventSignalInput{
			{SourceID: 99, Topic: "Distributed Systems", EventType: ExploreEventExposure, OccurredAt: now.Add(-time.Hour)},
			{SourceID: 99, Topic: "Distributed Systems", EventType: ExploreEventClick, OccurredAt: now.Add(-time.Hour)},
			{SourceID: 99, Topic: "Distributed Systems", EventType: ExploreEventCompletedRead, OccurredAt: now.Add(-time.Hour)},
			{SourceID: 100, Topic: "Expired", EventType: ExploreEventClick, OccurredAt: now.Add(-31 * 24 * time.Hour)},
		},
		Feedback: []ExplicitFeedbackInput{
			{SourceID: 42, Type: FeedbackHideSource},
			{Topic: " Distributed Systems ", Type: FeedbackBoostTopic},
			{Topic: "Noise", Type: FeedbackDampenTopic},
			{Topic: "noise", Type: FeedbackDampenTopic},
		},
	})

	if !reflect.DeepEqual(profile.VisibleSourceIDs, []int{7}) || !reflect.DeepEqual(profile.VisibleDomains, []string{"example.com"}) || !reflect.DeepEqual(profile.HiddenSourceIDs, []int{42}) {
		t.Fatalf("profile identity filters=%+v", profile)
	}
	if !reflect.DeepEqual(profile.BoostTopics, []string{"distributed systems"}) || !reflect.DeepEqual(profile.DampenTopics, []string{"noise"}) {
		t.Fatalf("feedback=%+v", profile)
	}
	assertSignalGreater(t, profile.SubscriptionSignals, SignalToken, "go", 0)
	assertSignalGreater(t, profile.SubscriptionSignals, SignalDomain, "example.com", 0)
	assertSignalGreater(t, profile.SubscriptionSignals, SignalTag, "backend", 0)
	assertSignalGreater(t, profile.BehaviorSignals, SignalTopic, "distributed systems", 0)
	assertSignalAbsent(t, profile.BehaviorSignals, "expired")
	assertSignalAbsent(t, profile.BehaviorSignals, "ignored")
	for _, signal := range append(append([]WeightedSignal(nil), profile.SubscriptionSignals...), profile.BehaviorSignals...) {
		if math.IsNaN(signal.Weight) || math.IsInf(signal.Weight, 0) || signal.Weight < 0 || signal.Value == "" {
			t.Fatalf("unsafe signal=%+v", signal)
		}
	}
	if len(profile.SubscriptionSignals) > MaxProfileSubscriptionSignals || len(profile.BehaviorSignals) > MaxProfileBehaviorSignals {
		t.Fatalf("unbounded profile subscription=%d behavior=%d", len(profile.SubscriptionSignals), len(profile.BehaviorSignals))
	}
}

func TestExploreProfilePublicInputsContainNoPrivateIdentityOrBody(t *testing.T) {
	for _, value := range []any{ProfileInput{}, SubscriptionSignalInput{}, RecentArticleSignalInput{}, ExploreEventSignalInput{}, ExploreProfile{}} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := typeOf.Field(index).Name
			if name == "UserID" || name == "Content" || name == "URL" || name == "FeedURL" || name == "SiteURL" {
				t.Fatalf("%s exposes private field %s", typeOf.Name(), name)
			}
		}
	}
}

func TestExploreProfileCapsLargeInputsBeforeTheyCanCrowdValidRecentSignals(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	input := ProfileInput{Now: now}
	for index := 0; index < 200; index++ {
		input.Subscriptions = append(input.Subscriptions, SubscriptionSignalInput{Title: "subscription " + string(rune('a'+index%26)), Tags: []string{"tag " + string(rune('a'+index%26))}})
		input.RecentArticles = append(input.RecentArticles, RecentArticleSignalInput{Title: "future", PublishedAt: now.Add(24 * time.Hour)})
		input.ExploreEvents = append(input.ExploreEvents, ExploreEventSignalInput{Topic: "future", EventType: ExploreEventClick, OccurredAt: now.Add(24 * time.Hour)})
	}
	input.RecentArticles = append(input.RecentArticles, RecentArticleSignalInput{Title: "valid recent", PublishedAt: now.Add(-time.Hour)})
	input.ExploreEvents = append(input.ExploreEvents, ExploreEventSignalInput{Topic: "valid event", EventType: ExploreEventClick, OccurredAt: now.Add(-time.Hour)})
	profile := BuildExploreProfile(input)
	if len(profile.SubscriptionSignals) > MaxProfileSubscriptionSignals || len(profile.BehaviorSignals) > MaxProfileBehaviorSignals {
		t.Fatalf("unbounded profile subscription=%d behavior=%d", len(profile.SubscriptionSignals), len(profile.BehaviorSignals))
	}
	assertSignalGreater(t, profile.BehaviorSignals, SignalToken, "valid", 0)
	assertSignalGreater(t, profile.BehaviorSignals, SignalTopic, "valid event", 0)
}

func assertSignalGreater(t *testing.T, signals []WeightedSignal, kind SignalKind, value string, minimum float64) {
	t.Helper()
	for _, signal := range signals {
		if signal.Kind == kind && signal.Value == value {
			if signal.Weight <= minimum {
				t.Fatalf("signal %+v <= %v", signal, minimum)
			}
			return
		}
	}
	t.Fatalf("missing signal %s:%s in %+v", kind, value, signals)
}

func assertSignalAbsent(t *testing.T, signals []WeightedSignal, value string) {
	t.Helper()
	for _, signal := range signals {
		if signal.Value == value {
			t.Fatalf("unexpected signal %+v", signal)
		}
	}
}
