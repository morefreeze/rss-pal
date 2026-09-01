package repository

import (
	"strings"
	"testing"
)

func TestExploreSubscriptionProfileSQLAggregatesOnlyVisibleRecentArticleMetadata(t *testing.T) {
	normalized := strings.Join(strings.Fields(exploreSubscriptionProfileSQL), " ")
	for _, fragment := range []string{
		"feed.owner_id=$1",
		"feed.owner_id IS NULL AND article.published_at IS NOT NULL",
		"article.published_at >= GREATEST($2,profile_user.shared_visible_from)",
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
	for _, fragment := range []string{
		"LEFT(COALESCE(article.content,''),4000)",
		"LEFT(COALESCE(article.summary_brief,''),1000)",
		"feed.owner_id IS NULL AND article.published_at IS NOT NULL",
		"article.published_at >= GREATEST($2,profile_user.shared_visible_from)",
		"LIMIT 200",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("recent article profile SQL missing %q: %s", fragment, normalized)
		}
	}
}
