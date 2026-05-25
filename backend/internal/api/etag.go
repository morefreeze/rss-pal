package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// ComputeListETag builds a weak ETag for an article-list response.
// Inputs combine a per-request query signature (so different filter
// combinations get distinct ETags) with content fingerprints — count,
// first/last id, and the max fetched_at across items. Cheap (no extra
// DB round trip) and changes whenever the worker writes new articles
// matching the query.
//
// Format: W/"<hex sha256 prefix>"
func ComputeListETag(querySignature string, items []ArticleListItem) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|count=%d|", querySignature, len(items))
	if len(items) > 0 {
		var maxFetched time.Time
		for _, it := range items {
			if it.FetchedAt.After(maxFetched) {
				maxFetched = it.FetchedAt
			}
		}
		fmt.Fprintf(h, "first=%d|last=%d|max_fetched=%d",
			items[0].ID, items[len(items)-1].ID, maxFetched.UnixNano())
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}

// ComputeDetailETag builds a weak ETag for a single-article response.
// Sensitive to fetched_at, content body, and summary bodies — any of
// which change when the worker re-fetches or re-summarises the article.
// Content/summaries are hashed (not just length-counted) so edits that
// preserve length still bust the cache.
func ComputeDetailETag(a model.Article) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|id=%d|fetched=%d|state=%s|", a.ID, a.FetchedAt.UnixNano(), a.ProcessingState)
	h.Write([]byte(a.Content))
	h.Write([]byte("|brief="))
	h.Write([]byte(a.SummaryBrief))
	h.Write([]byte("|detailed="))
	h.Write([]byte(a.SummaryDetailed))
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}
