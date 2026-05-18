package repository

import (
	"log"

	"github.com/bytedance/rss-pal/internal/model"
)

// combineLinkSetResults merges primary recommendations with a fallback batch.
//
// Primary articles are marked IsFallback=false. If len(primary) >= limit,
// fallbackFn is not invoked. Otherwise fallbackFn supplies up to
// (limit - len(primary)) extra articles, which are appended and marked
// IsFallback=true.
//
// If fallbackFn returns an error, it is logged and the primary slice is
// returned alone — fallback is best-effort and must never block a
// successful primary result.
func combineLinkSetResults(
	primary []model.Article,
	fallbackFn func() ([]model.Article, error),
	limit int,
) ([]model.Article, error) {
	for i := range primary {
		primary[i].IsFallback = false
	}
	if len(primary) >= limit {
		return primary, nil
	}
	fallback, err := fallbackFn()
	if err != nil {
		log.Printf("link_set fallback query failed: %v", err)
		return primary, nil
	}
	for i := range fallback {
		fallback[i].IsFallback = true
	}
	return append(primary, fallback...), nil
}

// collectArticleIDs returns the IDs of all articles in the slice, in order.
// Used to build the exclude-list passed to the fallback query.
func collectArticleIDs(articles []model.Article) []int {
	ids := make([]int, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	return ids
}
