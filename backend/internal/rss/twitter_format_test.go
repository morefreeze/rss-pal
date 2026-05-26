package rss

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTweetContent_FullCase(t *testing.T) {
	cap := &TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		PublishedAt:  time.Date(2026, 4, 23, 8, 0, 0, 0, time.UTC),
		TextMarkdown: "+1 to this excellent thread.",
		ImageURLs:    []string{"https://pbs.twimg.com/media/AAA111.jpg?name=large"},
		Quote: &Quote{
			URL:         "https://x.com/someone_else/status/3333333333333333333",
			Author:      "someone_else",
			DisplayName: "Other Person",
			PublishedAt: time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC),
			Excerpt:     "quoted tweet body — extracted as excerpt.",
		},
	}
	got := BuildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy) · 2026-04-23\n\n" +
		"+1 to this excellent thread.\n\n" +
		"![](https://pbs.twimg.com/media/AAA111.jpg?name=large)\n\n" +
		"**引用** [@someone_else (Other Person)](https://x.com/someone_else/status/3333333333333333333) · 2026-04-22\n\n" +
		"> quoted tweet body — extracted as excerpt."
	if got != want {
		t.Errorf("BuildTweetContent mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildQuoteSection_FallbackURLOnly(t *testing.T) {
	got := buildQuoteSection(&Quote{URL: "https://x.com/x/status/1"})
	if got != "引用: https://x.com/x/status/1" {
		t.Errorf("got %q", got)
	}
}

func TestBuildQuoteSection_NoExcerpt(t *testing.T) {
	got := buildQuoteSection(&Quote{
		URL:         "https://x.com/x/status/1",
		Author:      "x",
		DisplayName: "X User",
		PublishedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	want := "**引用** [@x (X User)](https://x.com/x/status/1) · 2026-05-01"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildQuoteSection_MultilineExcerpt(t *testing.T) {
	got := buildQuoteSection(&Quote{
		URL:     "https://x.com/x/status/1",
		Author:  "x",
		Excerpt: "first paragraph.\n\nsecond paragraph.",
	})
	want := "**引用** [@x](https://x.com/x/status/1)\n\n> first paragraph.\n> \n> second paragraph."
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildQuoteSection_NilOrEmpty(t *testing.T) {
	if got := buildQuoteSection(nil); got != "" {
		t.Errorf("nil quote should produce empty, got %q", got)
	}
	if got := buildQuoteSection(&Quote{}); got != "" {
		t.Errorf("empty URL should produce empty, got %q", got)
	}
}

func TestBuildTweetContent_ImageOnly(t *testing.T) {
	cap := &TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"https://pbs.twimg.com/media/IMG.jpg?name=large"},
	}
	got := BuildTweetContent(cap)
	want := "> @karpathy\n\n![](https://pbs.twimg.com/media/IMG.jpg?name=large)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetContent_XArticle(t *testing.T) {
	cap := &TweetCapture{
		Author:       "ashpreetbedi",
		DisplayName:  "Ashpreet Bedi",
		PublishedAt:  time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC),
		ArticleTitle: "Auto-Improving Software",
		TextMarkdown: "Coding agents have changed how we build software.",
	}
	got := BuildTweetContent(cap)
	want := "> @ashpreetbedi (Ashpreet Bedi) · 2026-05-08\n\n" +
		"# Auto-Improving Software\n\n" +
		"Coding agents have changed how we build software."
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetTitle_XArticle(t *testing.T) {
	cap := &TweetCapture{
		Author:       "ashpreetbedi",
		DisplayName:  "Ashpreet Bedi",
		ArticleTitle: "Auto-Improving Software",
		TextMarkdown: "Coding agents have changed how we build software.",
	}
	want := "Ashpreet Bedi · Auto-Improving Software"
	if got := BuildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetContent_NoTimestamp(t *testing.T) {
	cap := &TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "hi",
	}
	got := BuildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy)\n\nhi"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetTitle_ClauseBreakOnPeriod(t *testing.T) {
	cap := &TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "+1 to this excellent thread.",
	}
	want := "Andrej Karpathy · +1 to this excellent thread"
	if got := BuildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_ClauseBreakOnComma(t *testing.T) {
	cap := &TweetCapture{
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "This works really well btw, at the end of your query ask your LLM to structure as HTML",
	}
	want := "Andrej Karpathy · This works really well btw"
	if got := BuildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_HandlePrefixWhenNoDisplayName(t *testing.T) {
	cap := &TweetCapture{
		Author:       "karpathy",
		TextMarkdown: "hello world.",
	}
	if got := BuildTweetTitle(cap); got != "@karpathy · hello world" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ChineseClause(t *testing.T) {
	cap := &TweetCapture{
		DisplayName:  "艾略特",
		TextMarkdown: "今天凌晨北京时间下午，Andrej Karpathy 转发了一条推文。",
	}
	want := "艾略特 · 今天凌晨北京时间下午"
	if got := BuildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_NoBreakWordBoundary(t *testing.T) {
	cap := &TweetCapture{
		DisplayName:  "tester",
		TextMarkdown: strings.Repeat("word ", 30), // 150 chars, no clause break
	}
	got := BuildTweetTitle(cap)
	if !strings.HasPrefix(got, "tester · word") {
		t.Errorf("missing prefix: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing ellipsis: %q", got)
	}
	if strings.Contains(got, " · word ") && strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Errorf("trailing space before ellipsis: %q", got)
	}
}

func TestBuildTweetTitle_NoBreakNoSpace(t *testing.T) {
	long := strings.Repeat("a", 80) // 80 a's, no breaks, no spaces
	cap := &TweetCapture{TextMarkdown: long}
	got := BuildTweetTitle(cap)
	if len([]rune(got)) != 61 { // 60 a's + ellipsis
		t.Errorf("title rune len = %d, want 61; got %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("title should end with ellipsis: %q", got)
	}
}

func TestBuildTweetTitle_NewlinesFlatten(t *testing.T) {
	cap := &TweetCapture{TextMarkdown: "first line\nsecond line"}
	if got := BuildTweetTitle(cap); got != "first line second line" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ImageOnlyFallback(t *testing.T) {
	cap := &TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"x"},
	}
	if got := BuildTweetTitle(cap); got != "@karpathy 的推文" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ImageOnlyDisplayNameFallback(t *testing.T) {
	cap := &TweetCapture{
		Author:      "karpathy",
		DisplayName: "Andrej Karpathy",
		ImageURLs:   []string{"x"},
	}
	if got := BuildTweetTitle(cap); got != "Andrej Karpathy 的推文" {
		t.Errorf("got %q", got)
	}
}
