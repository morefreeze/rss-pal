package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRedditTopRepeatedArticleKeepsBoundedStrongestEvidence(t *testing.T) {
	var items []string
	for i := 0; i < 30; i++ {
		items = append(items, redditPost(fmt.Sprintf("post%d", i), fmt.Sprint(100+i), "https://blog.example/article", ""))
	}
	forward, stats, err := ParseRedditTop(Provider{Topic: "programming"}, redditListing(items...))
	if err != nil || len(forward) != 1 || stats.External != 30 || forward[0].OccurrenceCount != 30 {
		t.Fatalf("candidates=%+v stats=%+v err=%v", forward, stats, err)
	}
	if err := ValidateCandidate(forward[0]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(forward[0].Tags, ","), "score:129") {
		t.Fatal("strongest evidence lost")
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	reverse, _, err := ParseRedditTop(Provider{Topic: "programming"}, redditListing(items...))
	if err != nil || !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("order-dependent candidates: %+v %+v %v", forward, reverse, err)
	}
}

func redditListing(items ...string) []byte {
	return []byte(`{"kind":"Listing","data":{"children":[` + strings.Join(items, ",") + `]}}`)
}
func redditPost(id, score, target, extra string) string {
	return fmt.Sprintf(`{"kind":"t3","data":{"id":%q,"score":%s,"url":%q,"subreddit":"programming"%s}}`, id, score, target, extra)
}

func TestRedditTopThresholdAndProvenance(t *testing.T) {
	body := redditListing(redditPost("a", "99", "https://low.example/post", ""), redditPost("b", "100", "https://blog.example/post", ""), redditPost("c", "101", "https://other.example/post", ""), redditPost("d", "null", "https://missing.example/post", ""))
	got, stats, err := ParseRedditTop(Provider{Topic: "programming"}, body)
	if err != nil || len(got) != 2 || stats.Posts != 4 || stats.Qualified != 2 {
		t.Fatalf("got=%+v stats=%+v err=%v", got, stats, err)
	}
	if got[0].OccurrenceCount != 1 || !strings.Contains(strings.Join(got[0].Tags, ","), "score:100") || !strings.Contains(strings.Join(got[0].Tags, ","), "https://www.reddit.com/comments/b") {
		t.Fatalf("missing provenance: %+v", got[0])
	}
	for _, c := range got {
		if err := ValidateCandidate(c); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err = ParseRedditTop(Provider{Endpoint: "https://www.reddit.com/r/programming/top.json#min_score=101"}, body)
	if err != nil || len(got) != 1 {
		t.Fatalf("custom threshold: %v %v", got, err)
	}
}

func TestRedditTopRejectsNonArticleTargetsAndDeduplicatesPosts(t *testing.T) {
	body := redditListing(
		redditPost("a", "100", "https://blog.example/a", ""), redditPost("a", "100", "https://blog.example/a", ""),
		redditPost("b", "99", "https://blog.example/b", ""),
		redditPost("c", "100", "https://self.example/a", `,"is_self":true`),
		redditPost("d", "100", "https://www.reddit.com/r/programming/a", ""),
		redditPost("e", "100", "https://youtube.com/watch?v=a", ""),
		redditPost("f", "100", "https://arxiv.org/abs/123", ""),
		redditPost("g", "100", "http://127.0.0.1/a", ""),
		redditPost("h", "100", "https://img.example/a.jpg", ""),
		redditPost("i", "100", "https://video.example/a", `,"is_video":true`))
	got, stats, err := ParseRedditTop(Provider{}, body)
	if err != nil || len(got) != 1 || got[0].OccurrenceCount != 1 || stats.External != 1 {
		t.Fatalf("got=%+v stats=%+v err=%v", got, stats, err)
	}
}

func TestRedditTopRejectsInvalidDataAndThreshold(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `<html>blocked</html>`, `{"kind":"Listing","data":{}}`, string(redditListing()) + `{}`, strings.Repeat("x", defaultProviderBodyBytes+1)} {
		if _, _, err := ParseRedditTop(Provider{}, []byte(body)); err == nil {
			t.Fatalf("accepted invalid response len=%d", len(body))
		}
	}
	for _, fragment := range []string{"min_score=0", "min_score=-1", "min_score=no", "min_score=", "min_score=1&min_score=100", "unexpected=1"} {
		if _, _, err := ParseRedditTop(Provider{Endpoint: "https://www.reddit.com/#" + fragment}, redditListing()); err == nil {
			t.Fatalf("accepted %s", fragment)
		}
	}
	if _, _, err := ParseRedditTop(Provider{}, redditListing()); err != nil {
		t.Fatalf("valid empty listing: %v", err)
	}
}

func TestRedditTopSingleQualifiedPostSuppliesConfidence(t *testing.T) {
	now := time.Now()
	evidence := []ObservationEvidence{{ProviderID: 1, ProviderKind: "reddit_top", Enabled: true, LastSeenAt: now, ProviderLastSuccessAt: &now, OccurrenceCount: 1}}
	if !HasSourceConfidence(now, evidence, false) {
		t.Fatal("qualified Reddit post was rejected")
	}
	evidence[0].ProviderKind = "reddit_stream"
	if HasSourceConfidence(now, evidence, false) {
		t.Fatal("unscored Reddit post was accepted")
	}
}
