package repository

import "testing"

func TestArticleOrderClause(t *testing.T) {
	tests := []struct {
		name  string
		alias string
		sort  SortMode
		dir   SortDir
		want  string
	}{
		{
			name:  "formal captured descending",
			alias: ArticleAliasFormal,
			sort:  SortCaptured,
			dir:   SortDesc,
			want:  "ORDER BY articles.fetched_at DESC",
		},
		{
			name:  "explore captured ascending",
			alias: ArticleAliasExplore,
			sort:  SortCaptured,
			dir:   SortAsc,
			want:  "ORDER BY explore_articles.fetched_at ASC",
		},
		{
			name:  "formal published descending",
			alias: ArticleAliasFormal,
			sort:  SortPublished,
			dir:   SortDesc,
			want:  "ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(articles.published_at, articles.fetched_at), articles.fetched_at - INTERVAL '7 days')) DESC, COALESCE(articles.published_at, articles.fetched_at) DESC",
		},
		{
			name:  "explore published ascending",
			alias: ArticleAliasExplore,
			sort:  SortPublished,
			dir:   SortAsc,
			want:  "ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(explore_articles.published_at, explore_articles.fetched_at), explore_articles.fetched_at - INTERVAL '7 days')) ASC, COALESCE(explore_articles.published_at, explore_articles.fetched_at) ASC",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArticleOrderClause(tt.alias, tt.sort, tt.dir); got != tt.want {
				t.Fatalf("ArticleOrderClause() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArticleOrderClauseRejectsUntrustedAlias(t *testing.T) {
	if got := ArticleOrderClause("articles; DROP TABLE users", SortCaptured, SortDesc); got != "" {
		t.Fatalf("untrusted alias produced SQL: %q", got)
	}
}
