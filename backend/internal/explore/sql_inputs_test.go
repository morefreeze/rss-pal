package explore

import (
	"strings"
	"testing"
	"time"
)

func TestFreshObservationWindowUsesTwiceProviderIntervalWithSixHourFloor(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    time.Duration
	}{{30, 6 * time.Hour}, {180, 6 * time.Hour}, {360, 12 * time.Hour}, {1440, 48 * time.Hour}} {
		if got := FreshObservationWindow(tc.minutes); got != tc.want {
			t.Fatalf("minutes=%d window=%s want=%s", tc.minutes, got, tc.want)
		}
	}
}

func TestClipProfileTextBoundsUnicodeWithoutSplittingRunes(t *testing.T) {
	value := strings.Repeat("界", MaxProfileContentRunes) + "尾"
	got := ClipProfileText(value, MaxProfileContentRunes)
	if len([]rune(got)) != MaxProfileContentRunes || !strings.HasSuffix(got, "界") {
		t.Fatalf("clipped rune count=%d suffix=%q", len([]rune(got)), got[len(got)-3:])
	}
}

func TestRecentArticleSignalsIncludeTopicTagsContentAndSnippet(t *testing.T) {
	profile := BuildExploreProfile(ProfileInput{Now: time.Now(), RecentArticles: []RecentArticleSignalInput{{
		Title: "weekly", Category: "technology", Topic: "distributed systems",
		Tags: []string{"consensus"}, TextTokens: ProfileTextTokens("raft replication", "fault tolerance"),
		PublishedAt: time.Now().Add(-time.Hour),
	}}})
	for _, want := range []string{"distributed", "consensus", "raft", "fault"} {
		found := false
		for _, signal := range profile.SubscriptionSignals {
			if signal.Value == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing high-weight recent article signal %q: %+v", want, profile.SubscriptionSignals)
		}
	}
}
