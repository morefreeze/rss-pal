package repository

import (
	"strings"
	"testing"
)

func TestExploreSubscriptionProfileSQLAggregatesOnlyVisibleRecentArticleMetadata(t *testing.T) {
	normalized := strings.Join(strings.Fields(exploreSubscriptionProfileSQL), " ")
	if strings.Contains(normalized, "owner_id IS NULL") {
		t.Fatal("profile leaks unowned subscriptions")
	}
	for _, fragment := range []string{
		"feed.owner_id=$1",
		"COALESCE(article.published_at,article.fetched_at) >= $2",
		"article.category",
		"unnest(COALESCE(article.tags,'{}'))",
		"LIMIT 20",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("subscription profile SQL missing %q: %s", fragment, normalized)
		}
	}
}

func TestExploreRecentArticleProfileSQLKeepsContentProjectionBounded(t *testing.T) {
	normalized := strings.Join(strings.Fields(exploreRecentArticleProfileSQL), " ")
	if strings.Contains(normalized, "owner_id IS NULL") {
		t.Fatal("profile leaks unowned subscriptions")
	}
	for _, fragment := range []string{
		"LEFT(COALESCE(article.content,''),4000)",
		"LEFT(COALESCE(article.summary_brief,''),1000)",
		"LIMIT 200",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("recent article profile SQL missing %q: %s", fragment, normalized)
		}
	}
}
