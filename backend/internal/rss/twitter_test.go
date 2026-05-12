package rss

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIsTwitterStatusURL(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantID string
		wantOK bool
	}{
		{"x.com status", "https://x.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"twitter.com status", "https://twitter.com/x/status/1", "1", true},
		{"mobile.twitter.com status", "https://mobile.twitter.com/x/status/42", "42", true},
		{"www.x.com status", "https://www.x.com/x/status/9", "9", true},
		{"uppercase host", "https://X.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"trailing slash", "https://x.com/karpathy/status/123/", "123", true},
		{"with query (already normalized away, but accept)", "https://x.com/x/status/1?s=20", "1", true},
		{"profile page", "https://x.com/karpathy", "", false},
		{"with_replies", "https://x.com/karpathy/with_replies", "", false},
		{"search page", "https://x.com/search?q=go", "", false},
		{"lists page", "https://x.com/i/lists/1234", "", false},
		{"non-numeric status id", "https://x.com/karpathy/status/abc", "", false},
		{"status without id", "https://x.com/karpathy/status/", "", false},
		{"non-twitter host", "https://example.com/karpathy/status/1", "", false},
		{"empty", "", "", false},
		{"unparseable", "not a url", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := IsTwitterStatusURL(tt.in)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("IsTwitterStatusURL(%q) = (%q, %v); want (%q, %v)", tt.in, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestExtractTweet_FocalSelection(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap == nil {
		t.Fatal("got nil capture")
	}
}

func TestExtractTweet_FocalNotInHTML(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	_, err := ExtractTweet(data, "1111111111111111111")
	if !errors.Is(err, ErrTweetNotFound) {
		t.Fatalf("want ErrTweetNotFound, got %v", err)
	}
}

func TestExtractTweet_TextOnly(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "The biggest unlock from LLMs for me has been the [blog post](https://example.com/blog) on building intuition."
	if cap.TextMarkdown != want {
		t.Errorf("TextMarkdown mismatch\n got: %q\nwant: %q", cap.TextMarkdown, want)
	}
}

func TestExtractTweet_AuthorAndTimestamp(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Author != "karpathy" {
		t.Errorf("Author = %q, want %q", cap.Author, "karpathy")
	}
	if cap.DisplayName != "Andrej Karpathy" {
		t.Errorf("DisplayName = %q, want %q", cap.DisplayName, "Andrej Karpathy")
	}
	wantTime := time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC)
	if !cap.PublishedAt.Equal(wantTime) {
		t.Errorf("PublishedAt = %v, want %v", cap.PublishedAt, wantTime)
	}
}

func TestExtractTweet_Images(t *testing.T) {
	data := mustReadFixture(t, "tweet_with_images.html")
	cap, err := ExtractTweet(data, "2222222222222222222")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://pbs.twimg.com/media/AAA111.jpg?format=jpg&name=large",
		"https://pbs.twimg.com/media/BBB222.jpg?format=jpg&name=large",
	}
	if len(cap.ImageURLs) != len(want) {
		t.Fatalf("ImageURLs len = %d, want %d: %v", len(cap.ImageURLs), len(want), cap.ImageURLs)
	}
	for i := range want {
		if cap.ImageURLs[i] != want[i] {
			t.Errorf("ImageURLs[%d] = %q, want %q", i, cap.ImageURLs[i], want[i])
		}
	}
}

func TestExtractTweet_QuoteURL(t *testing.T) {
	data := mustReadFixture(t, "tweet_with_quote.html")
	cap, err := ExtractTweet(data, "2053872850101285137")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://x.com/someone_else/status/3333333333333333333"
	if cap.QuoteURL != want {
		t.Errorf("QuoteURL = %q, want %q", cap.QuoteURL, want)
	}
	// The quoted tweet's text must NOT leak into our TextMarkdown.
	if strings.Contains(cap.TextMarkdown, "quoted tweet body") {
		t.Errorf("quoted tweet body leaked into TextMarkdown: %q", cap.TextMarkdown)
	}
}

func TestExtractTweet_ImageOnly(t *testing.T) {
	data := mustReadFixture(t, "tweet_image_only.html")
	cap, err := ExtractTweet(data, "4444444444444444444")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.TextMarkdown != "" {
		t.Errorf("TextMarkdown should be empty, got %q", cap.TextMarkdown)
	}
	if len(cap.ImageURLs) != 1 {
		t.Errorf("want 1 image, got %d", len(cap.ImageURLs))
	}
}

func mustReadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/twitter/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
