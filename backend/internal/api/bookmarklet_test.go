package api

import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/rss"
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
		{
			// WeChat-style lazy-load: real image URL is in data-src, src is
			// missing entirely (or a 1×1 placeholder). Without lazy-attr
			// promotion the article would render with no images at all.
			name:    "wechat data-src promoted to img",
			baseURL: "https://mp.weixin.qq.com/s/abc",
			html: `<html><body><div id="js_content">
				<p>开头段落足够长以便选择器命中正文部分内容内容内容内容内容内容内容内容内容内容内容内容内容内容。</p>
				<p><img class="rich_pages wxw-img" data-src="https://mmbiz.qpic.cn/foo.png" data-type="png"></p>
				<p>尾部段落同样足够长以便保证整体长度满足提取阈值内容内容内容内容内容内容内容内容内容内容。</p>
			</div></body></html>`,
			mustHave: []string{"https://mmbiz.qpic.cn/foo.png"},
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

// TestExtractContentFromHTML_PreservesContainerWithJunkClass guards against
// the cleanup phase wiping a top-level container (html/body/head/main/article)
// just because its class attribute happens to match an attribute-substring
// selector. WeChat sets <body class="… comment_feature …"> which used to be
// matched by [class*=comment] and dropped, returning empty content.
func TestExtractContentFromHTML_PreservesContainerWithJunkClass(t *testing.T) {
	html := `<html><body class="zh_CN wx_wap_page mm_appmsg comment_feature discuss_tab"><div id="js_content">
		<p>开头段落足够长以便选择器命中正文部分内容内容内容内容内容内容内容内容内容内容内容内容内容内容。</p>
		<p>第二段也足够长以保证最终提取的正文长度超过判定阈值内容内容内容内容内容内容内容内容内容。</p>
	</div></body></html>`
	got, err := extractContentFromHTML(html, "https://mp.weixin.qq.com/s/abc")
	if err != nil {
		t.Fatalf("extractContentFromHTML: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatalf("expected non-empty content, got empty")
	}
	for _, want := range []string{"开头段落", "第二段"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
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

func TestBuildTweetContent_FullCase(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		PublishedAt:  time.Date(2026, 4, 23, 8, 0, 0, 0, time.UTC),
		TextMarkdown: "+1 to this excellent thread.",
		ImageURLs:    []string{"https://pbs.twimg.com/media/AAA111.jpg?name=large"},
		Quote: &rss.Quote{
			URL:         "https://x.com/someone_else/status/3333333333333333333",
			Author:      "someone_else",
			DisplayName: "Other Person",
			PublishedAt: time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC),
			Excerpt:     "quoted tweet body — extracted as excerpt.",
		},
	}
	got := buildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy) · 2026-04-23\n\n" +
		"+1 to this excellent thread.\n\n" +
		"![](https://pbs.twimg.com/media/AAA111.jpg?name=large)\n\n" +
		"**引用** [@someone_else (Other Person)](https://x.com/someone_else/status/3333333333333333333) · 2026-04-22\n\n" +
		"> quoted tweet body — extracted as excerpt."
	if got != want {
		t.Errorf("buildTweetContent mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildQuoteSection_FallbackURLOnly(t *testing.T) {
	got := buildQuoteSection(&rss.Quote{URL: "https://x.com/x/status/1"})
	if got != "引用: https://x.com/x/status/1" {
		t.Errorf("got %q", got)
	}
}

func TestBuildQuoteSection_NoExcerpt(t *testing.T) {
	got := buildQuoteSection(&rss.Quote{
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
	got := buildQuoteSection(&rss.Quote{
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
	if got := buildQuoteSection(&rss.Quote{}); got != "" {
		t.Errorf("empty URL should produce empty, got %q", got)
	}
}

func TestBuildTweetContent_ImageOnly(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"https://pbs.twimg.com/media/IMG.jpg?name=large"},
	}
	got := buildTweetContent(cap)
	want := "> @karpathy\n\n![](https://pbs.twimg.com/media/IMG.jpg?name=large)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetContent_NoTimestamp(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "hi",
	}
	got := buildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy)\n\nhi"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetTitle_ClauseBreakOnPeriod(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "+1 to this excellent thread.",
	}
	want := "Andrej Karpathy · +1 to this excellent thread"
	if got := buildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_ClauseBreakOnComma(t *testing.T) {
	cap := &rss.TweetCapture{
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "This works really well btw, at the end of your query ask your LLM to structure as HTML",
	}
	want := "Andrej Karpathy · This works really well btw"
	if got := buildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_HandlePrefixWhenNoDisplayName(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		TextMarkdown: "hello world.",
	}
	if got := buildTweetTitle(cap); got != "@karpathy · hello world" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ChineseClause(t *testing.T) {
	cap := &rss.TweetCapture{
		DisplayName:  "艾略特",
		TextMarkdown: "今天凌晨北京时间下午，Andrej Karpathy 转发了一条推文。",
	}
	want := "艾略特 · 今天凌晨北京时间下午"
	if got := buildTweetTitle(cap); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildTweetTitle_NoBreakWordBoundary(t *testing.T) {
	cap := &rss.TweetCapture{
		DisplayName:  "tester",
		TextMarkdown: strings.Repeat("word ", 30), // 150 chars, no clause break
	}
	got := buildTweetTitle(cap)
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
	cap := &rss.TweetCapture{TextMarkdown: long}
	got := buildTweetTitle(cap)
	if len([]rune(got)) != 61 { // 60 a's + ellipsis
		t.Errorf("title rune len = %d, want 61; got %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("title should end with ellipsis: %q", got)
	}
}

func TestBuildTweetTitle_NewlinesFlatten(t *testing.T) {
	cap := &rss.TweetCapture{TextMarkdown: "first line\nsecond line"}
	if got := buildTweetTitle(cap); got != "first line second line" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ImageOnlyFallback(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"x"},
	}
	if got := buildTweetTitle(cap); got != "@karpathy 的推文" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ImageOnlyDisplayNameFallback(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:      "karpathy",
		DisplayName: "Andrej Karpathy",
		ImageURLs:   []string{"x"},
	}
	if got := buildTweetTitle(cap); got != "Andrej Karpathy 的推文" {
		t.Errorf("got %q", got)
	}
}
