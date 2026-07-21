package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ComputeListETag builds a weak ETag for an article-list response.
// Inputs combine a per-request query signature (so different filter
// combinations get distinct ETags) with content fingerprints — count,
// first/last id, max fetched_at, and a hash of every item's
// processing_state. The state hash is required: when an article
// transitions processing→ready (or →failed) the row's fetched_at does
// NOT change, but the UI's badge does. Without it the list would
// continue serving 304s with stale 处理中 badges forever.
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
		fmt.Fprintf(h, "first=%d|last=%d|max_fetched=%d|",
			items[0].ID, items[len(items)-1].ID, maxFetched.UnixNano())
		// Per-item state digest — cheap (single string per row) and
		// catches every processing_state transition in the cached set.
		for _, it := range items {
			fmt.Fprintf(h, "%d=%s;", it.ID, it.ProcessingState)
		}
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}

// MarshalDetailResponse serializes the complete private detail response and
// derives its weak validator from those exact bytes. This keeps link-set
// flags, children, progress, signals, hidden state, and article content in the
// same cache contract as the body sent to the client.
func MarshalDetailResponse(response any) ([]byte, string, error) {
	body, err := json.Marshal(response)
	if err != nil {
		return nil, "", err
	}
	h := sha256.New()
	h.Write([]byte("detail-v2|"))
	h.Write(body)
	return body, `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}
