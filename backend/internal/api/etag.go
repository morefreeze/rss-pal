package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
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
