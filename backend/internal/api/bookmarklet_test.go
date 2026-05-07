package api

import (
	"strings"
	"testing"
)

func TestExtractContentFromHTMLPreservesImages(t *testing.T) {
	cases := []struct {
		name     string
		baseURL  string
		html     string
		mustHave []string // substrings that must appear in extracted markdown
	}{
		{
			name:    "absolute img inside article",
			baseURL: "https://example.com/post/1",
			html: `<html><body><article>
				<h1>Title</h1>
				<p>Some paragraph long enough to pass the length filter for plain text fallback.</p>
				<p><img src="https://cdn.example.com/photo.jpg" alt="cat"></p>
				<p>Another paragraph long enough to keep the article above 200 chars total.</p>
			</article></body></html>`,
			mustHave: []string{"![cat](https://cdn.example.com/photo.jpg)"},
		},
		{
			name:    "relative img resolved against base url",
			baseURL: "https://example.com/post/1",
			html: `<html><body><article>
				<h1>Hello</h1>
				<p>Body paragraph one with enough characters to pass the 200-char gate easily right here.</p>
				<p><img src="/static/cat.jpg" alt="cat"></p>
				<p>Body paragraph two with enough characters to pass the 200-char gate easily right here.</p>
			</article></body></html>`,
			mustHave: []string{"![cat](https://example.com/static/cat.jpg)"},
		},
		{
			name:    "protocol-relative img resolved",
			baseURL: "https://example.com/post/1",
			html: `<html><body><article>
				<p>Body paragraph one with enough characters to pass the 200-char gate easily right here.</p>
				<p><img src="//cdn.example.com/pic.png" alt="pic"></p>
				<p>Body paragraph two with enough characters to pass the 200-char gate easily right here.</p>
			</article></body></html>`,
			mustHave: []string{"![pic](https://cdn.example.com/pic.png)"},
		},
		{
			name:    "multiple images preserved",
			baseURL: "https://example.com/post/1",
			html: `<html><body><article>
				<p>Body paragraph one with enough characters to pass the 200-char gate easily right here.</p>
				<p><img src="https://cdn.example.com/a.jpg" alt="a"></p>
				<p>Body paragraph two with enough characters to pass the 200-char gate easily right here.</p>
				<p><img src="https://cdn.example.com/b.jpg" alt="b"></p>
			</article></body></html>`,
			mustHave: []string{"![a](https://cdn.example.com/a.jpg)", "![b](https://cdn.example.com/b.jpg)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractContentFromHTML(tc.html, tc.baseURL)
			if err != nil {
				t.Fatalf("extractContentFromHTML returned error: %v", err)
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("extracted content missing %q\n--- got ---\n%s", want, got)
				}
			}
			if n := countMarkdownImages(got); n < len(tc.mustHave) {
				t.Errorf("countMarkdownImages = %d, want >= %d\n--- got ---\n%s", n, len(tc.mustHave), got)
			}
		})
	}
}

func TestShouldPromptDuplicate(t *testing.T) {
	cases := []struct {
		name      string
		newLen    int
		oldLen    int
		newImages int
		oldImages int
		force     bool
		expected  bool
	}{
		// force always wins
		{"force overrides everything", 100, 1000, 0, 5, true, false},
		{"force on improvement still passes through", 5000, 1000, 5, 5, true, false},

		// oldLen == 0 means no real prior content; auto-overwrite
		{"old empty, any new", 0, 0, 0, 0, false, false},
		{"old empty, new has content", 100, 0, 1, 0, false, false},

		// 1.5x boundary, equal images: clear length improvement skips prompt
		{"new exactly 1.5x triggers no prompt", 1500, 1000, 2, 2, false, false},
		{"new just above 1.5x", 1501, 1000, 2, 2, false, false},
		{"new far above 1.5x", 5000, 1000, 5, 2, false, false},

		// length below 1.5x, equal images: length triggers prompt
		{"new just below 1.5x prompts", 1499, 1000, 2, 2, false, true},
		{"new equal to old prompts", 1000, 1000, 2, 2, false, true},
		{"new shorter than old prompts", 500, 1000, 2, 2, false, true},
		{"new much shorter prompts", 100, 1000, 0, 0, false, true},

		// image regression: even with length improvement, dropped images prompt
		{"length 2x but lost an image prompts", 2000, 1000, 1, 2, false, true},
		{"length 5x but lost all images prompts", 5000, 1000, 0, 3, false, true},
		{"length 1.5x but image dropped 3->2 prompts", 1500, 1000, 2, 3, false, true},

		// image stays the same or grows: no prompt as long as length passes
		{"length 1.5x and gained an image", 1500, 1000, 3, 2, false, false},
		{"length 2x and same image count", 2000, 1000, 2, 2, false, false},

		// image regression but length also fails: still prompts
		{"both length and image regression prompts", 500, 1000, 0, 2, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldPromptDuplicate(tc.newLen, tc.oldLen, tc.newImages, tc.oldImages, tc.force)
			if got != tc.expected {
				t.Errorf("shouldPromptDuplicate(newLen=%d, oldLen=%d, newImg=%d, oldImg=%d, force=%v) = %v, want %v",
					tc.newLen, tc.oldLen, tc.newImages, tc.oldImages, tc.force, got, tc.expected)
			}
		})
	}
}

func TestCountMarkdownImages(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"no images", "Just text with no images.\n\nMore text.", 0},
		{"one image", "Hello ![cat](https://example.com/cat.png) world", 1},
		{"three images", "![a](a.png) some text ![b](b.png) more ![c](c.png)", 3},
		{"image with empty alt", "![](pic.jpg)", 1},
		{"image inside paragraph", "Paragraph with image:\n\n![hi](x.png)\n\nMore.", 1},
		{"adjacent without space", "![a](a)![b](b)", 2},
		{"link not image", "[not-an-image](http://example.com)", 0},
		{"image url with parens guarded by closing paren", "![alt](a.png) and ![b](b.png)", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countMarkdownImages(tc.in)
			if got != tc.want {
				t.Errorf("countMarkdownImages(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
