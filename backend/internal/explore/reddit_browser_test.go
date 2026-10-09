package explore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRedditBrowserBatchValidation(t *testing.T) {
	now := time.Now().UTC()
	body := json.RawMessage(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"abc","score":100,"subreddit":"programming","url":"https://blog.example/post"}}]}}`)
	batch := RedditBrowserBatch{Subreddit: "programming", Period: "week", CapturedAt: now, Listing: body}
	provider := Provider{Endpoint: RedditTopEndpoint("programming", "week", 100), Topic: "programming"}
	candidates, stats, err := batch.Parse(provider, now)
	if err != nil || len(candidates) != 1 || stats.Qualified != 1 {
		t.Fatalf("candidates=%v stats=%+v err=%v", candidates, stats, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*RedditBrowserBatch)
	}{
		{"unknown board", func(b *RedditBrowserBatch) { b.Subreddit = "all" }},
		{"unknown period", func(b *RedditBrowserBatch) { b.Period = "all" }},
		{"stale", func(b *RedditBrowserBatch) { b.CapturedAt = now.Add(-13 * time.Hour) }},
		{"future", func(b *RedditBrowserBatch) { b.CapturedAt = now.Add(6 * time.Minute) }},
		{"mismatched board", func(b *RedditBrowserBatch) {
			b.Listing = json.RawMessage(strings.ReplaceAll(string(body), "programming", "other"))
		}},
		{"too many", func(b *RedditBrowserBatch) {
			var v map[string]any
			_ = json.Unmarshal(body, &v)
			data := v["data"].(map[string]any)
			child := data["children"].([]any)[0]
			children := make([]any, 101)
			for i := range children {
				children[i] = child
			}
			data["children"] = children
			b.Listing, _ = json.Marshal(v)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := batch
			tc.mutate(&b)
			if _, _, err := b.Parse(provider, now); err == nil {
				t.Fatal("accepted invalid batch")
			}
		})
	}
	for _, score := range []int{99, 100, 101} {
		b := batch
		var listing map[string]any
		_ = json.Unmarshal(body, &listing)
		listing["data"].(map[string]any)["children"].([]any)[0].(map[string]any)["data"].(map[string]any)["score"] = score
		b.Listing, _ = json.Marshal(listing)
		cs, _, err := b.Parse(provider, now)
		if err != nil || (len(cs) == 1) != (score >= 100) {
			t.Fatalf("score=%d candidates=%v err=%v", score, cs, err)
		}
	}
	provider.Endpoint = RedditTopEndpoint("programming", "week", 1)
	if _, _, err := batch.Parse(provider, now); err == nil {
		t.Fatal("accepted server threshold below hard minimum")
	}
}

func TestRedditBrowserDynamicNames(t *testing.T) {
	for _, name := range []string{"golang", "Rust", "learn_programming"} {
		key, err := (RedditBrowserBatch{Subreddit: name, Period: "week"}).ProviderKey()
		if err != nil || key != "reddit-"+strings.ToLower(name)+"-top-week" {
			t.Fatalf("name=%s key=%s err=%v", name, key, err)
		}
	}
	for _, name := range []string{"all", "popular", "friends", "mod", "u_someone", "go+rust", "../go", "go?x=1", "", strings.Repeat("a", 22)} {
		if _, err := (RedditBrowserBatch{Subreddit: name, Period: "week"}).ProviderKey(); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
