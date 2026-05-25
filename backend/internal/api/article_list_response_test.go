package api_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestArticleListItemHasNoFatPayload is a marshal-shape test: the
// JSON keys returned by GET /api/articles must NOT include `content`
// or `summary_detailed`. We construct the DTO directly rather than
// spinning up a real handler — the SQL stack needs a DB.
func TestArticleListItemHasNoFatPayload(t *testing.T) {
	item := newArticleListItemForTest()
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{`"content"`, `"summary_detailed"`} {
		if strings.Contains(string(b), banned) {
			t.Fatalf("list item JSON must not contain %s; got %s", banned, b)
		}
	}
	for _, required := range []string{`"id"`, `"title"`, `"url"`, `"summary_brief"`, `"fetched_at"`, `"manual_tags"`} {
		if !strings.Contains(string(b), required) {
			t.Fatalf("list item JSON missing %s; got %s", required, b)
		}
	}
}
