package rss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type hnTransport func(*http.Request) (*http.Response, error)

func (f hnTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func hnTestFetcher(items map[string]string) *ContentFetcher {
	return &ContentFetcher{client: &http.Client{Transport: hnTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "hacker-news.firebaseio.com" {
			return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
		}
		body, ok := items[r.URL.Path]
		if !ok {
			return nil, fmt.Errorf("missing fixture %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}
func TestHNFetchStoryAndRankedDiscussion(t *testing.T) {
	f := hnTestFetcher(map[string]string{
		"/v0/item/123.json": `{"id":123,"type":"story","title":"Ask HN: Games?","by":"alice","score":19,"descendants":2,"text":"<p>What do you play?</p>","kids":[124]}`,
		"/v0/item/124.json": `{"id":124,"type":"comment","by":"bob","time":1700000000,"text":"<p>Try <a href=\"https://example.com/game\">this game</a>.</p>","kids":[125]}`,
		"/v0/item/125.json": `{"id":125,"type":"comment","by":"carol","text":"Good suggestion!"}`,
	})
	got, err := f.FetchContentWithMetadata(context.Background(), "https://news.ycombinator.com/item?id=123&utm_source=test#comments")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Ask HN: Games?" {
		t.Fatalf("title=%q", got.Title)
	}
	for _, want := range []string{"What do you play?", "bob", "carol", "Good suggestion!", "https://news.ycombinator.com/item?id=124", "https://example.com/game", "19", "2"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q: %s", want, got.Content)
		}
	}
}
func TestHNFetchFailureDoesNotSaveGenericPage(t *testing.T) {
	for _, body := range []string{`null`, `{"id":123,"type":"comment","text":"not a story"}`, `{"id":123,"type":"story","deleted":true}`, `{"id":123,"type":"story","title":"valid","kids":[124]}`} {
		f := hnTestFetcher(map[string]string{"/v0/item/123.json": body})
		if _, err := f.FetchContent(context.Background(), "https://news.ycombinator.com/item?id=123"); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
func TestHNURLBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{"http://news.ycombinator.com/item?id=00123&utm_source=x#c", "https://news.ycombinator.com/item?id=123", true},
		{"https://news.ycombinator.com/item?id=0", "", true},
		{"https://news.ycombinator.com/item?id=123&id=456", "", true},
		{"https://news.ycombinator.com.evil/item?id=123", "", false},
		{"https://news.ycombinator.com:123/item?id=123", "", false},
		{"https://news.ycombinator.com/news", "", false},
	} {
		got, ok := HackerNewsItemURL(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s => %s,%v", tc.raw, got, ok)
		}
	}
}
func TestHNBoundsAndOmission(t *testing.T) {
	items := map[string]string{}
	roots := []string{}
	for i := int64(2); i <= 22; i++ {
		roots = append(roots, fmt.Sprint(i))
		kids := []string{}
		for j := int64(0); j < 12; j++ {
			cid := i*100 + j
			kids = append(kids, fmt.Sprint(cid))
			items[fmt.Sprintf("/v0/item/%d.json", cid)] = fmt.Sprintf(`{"id":%d,"type":"comment","by":"reply","text":"reply body"}`, cid)
		}
		items[fmt.Sprintf("/v0/item/%d.json", i)] = fmt.Sprintf(`{"id":%d,"type":"comment","by":"root","text":"root body","kids":[%s]}`, i, strings.Join(kids, ","))
	}
	items["/v0/item/1.json"] = `{"id":1,"type":"story","title":"Bounds","kids":[` + strings.Join(roots, ",") + `]}`
	f := hnTestFetcher(items)
	var calls atomic.Int32
	transport := f.client.Transport
	f.client.Transport = hnTransport(func(r *http.Request) (*http.Response, error) { calls.Add(1); return transport.RoundTrip(r) })
	got, err := f.FetchContent(context.Background(), "https://news.ycombinator.com/item?id=1")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 121 {
		t.Errorf("requests=%d, want 121", calls.Load())
	}
	if n := strings.Count(got, "· [原评论]"); n != 120 {
		t.Errorf("rendered=%d, want 120", n)
	}
	if !strings.Contains(got, "1 个讨论分支未展开") || !strings.Contains(got, "2 条回复未展开") {
		t.Fatalf("omissions missing: %s", got)
	}
}

func TestHNLiveCapture(t *testing.T) {
	if os.Getenv("RSSPAL_HN_LIVE_TEST") != "1" {
		t.Skip("opt-in public API check")
	}
	result, err := NewContentFetcher().FetchContentWithMetadata(context.Background(), "https://news.ycombinator.com/item?id=49985502")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Title, "games") || !strings.Contains(result.Content, "原评论") {
		t.Fatalf("incomplete capture: title=%s", result.Title)
	}
	t.Logf("title=%s bytes=%d comments=%d", result.Title, len(result.Content), strings.Count(result.Content, "· [原评论]"))
}
