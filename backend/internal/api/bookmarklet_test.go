package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
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

func TestExtractContentFromHTMLPrefersUserHTMLOverShareChrome(t *testing.T) {
	html := `<html><body>
		<div style="display:flex">
			<div class="text-minor text-center">分享至
				<img src="/assets/icons/wechat.svg">
				<a href="https://service.weibo.com/share/share.php"><img src="/assets/icons/weibo.svg"></a>
				<a href="https://connect.qq.com/widget/shareqq/index.html"><img src="/assets/icons/qq.svg"></a>
			</div>
			<div>
				<span class="user-html">
					<p>正文开头。这一段刻意写得足够长，用来模拟领研网页中真正的文章正文，并确保正文选择器可以独立通过长度阈值，而不需要退回到包含页面工具栏和推荐内容的 body 节点。</p>
					<p>正文结尾。这一段继续补充足够多的文字，验证抓取结果只包含 user-html 内的文章内容，不包含外层分享按钮、收藏按钮、评论区域或其他页面装饰。</p>
				</span>
				<div>热门推荐</div>
			</div>
		</div>
	</body></html>`

	got, err := extractContentFromHTML(html, "https://www.linkresearcher.com/careers/example")
	if err != nil {
		t.Fatalf("extractContentFromHTML: %v", err)
	}
	for _, want := range []string{"正文开头", "正文结尾"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing article text %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"分享至", "wechat.svg", "weibo.svg", "qq.svg", "热门推荐"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected page chrome %q in:\n%s", unwanted, got)
		}
	}
}

func TestExtractContentFromHTMLPrefersBlogEntryOverArchiveFooter(t *testing.T) {
	html := `<html><body>
		<div class="entry entryPage">
			<h2>Article title</h2>
			<p>The article discusses work completed in 2024, which is legitimate正文 and must stay in the capture.</p>
			<p>Its <a href="https://example.com/reference">reference link</a> is also part of the article正文.</p>
		</div>
		<div class="recent-articles"><h2>More recent articles</h2><a href="/newer">Newer post</a></div>
		<div id="ft"><a href="/2002/">2002</a><a href="/2003/">2003</a><a href="/2026/">2026</a></div>
	</body></html>`

	got, err := extractContentFromHTML(html, "https://example.com/post")
	if err != nil {
		t.Fatalf("extractContentFromHTML: %v", err)
	}
	for _, want := range []string{"Article title", "in 2024", "reference link"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing article content %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"More recent articles", "2002", "2003", "2026"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected page chrome %q in:\n%s", unwanted, got)
		}
	}
}

func TestExtractContentFromHTMLPreservesFullUTF8ContentAboveLegacyLimit(t *testing.T) {
	const tailMarker = "CHAPTER-END-完整"
	body := strings.Repeat("中", 16667) + tailMarker
	html := `<html><body><article><p>` + body + `</p></article></body></html>`

	got, err := extractContentFromHTML(html, "https://example.com/book/chapter1/")
	if err != nil {
		t.Fatalf("extractContentFromHTML: %v", err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("extracted content is not valid UTF-8 near the legacy 50,000-byte boundary")
	}
	if len(got) <= 50000 {
		t.Fatalf("extracted content length = %d, want > 50000", len(got))
	}
	if !strings.Contains(got, tailMarker) {
		t.Fatalf("extracted content lost tail marker %q", tailMarker)
	}
	if strings.HasSuffix(got, "...") {
		t.Fatalf("extracted content still has the legacy truncation suffix")
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

func TestArticleKind(t *testing.T) {
	if got := articleKind(true); got != "tweet" {
		t.Errorf("articleKind(true) = %q, want tweet", got)
	}
	if got := articleKind(false); got != "article" {
		t.Errorf("articleKind(false) = %q, want article", got)
	}
}

// TestCapture_KindSetByURL exercises the bookmarklet Capture handler end-to-end
// against in-memory stub repos. A twitter status URL must produce an article
// with Kind="tweet"; any other URL must produce Kind="article". This protects
// the discriminator wiring (B3) from silent regressions when the handler is
// refactored later — the model field, the handler branch, and the repo round-
// trip all have to line up for the assertions below to pass.
func TestCapture_KindSetByURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Mirrors backend/internal/rss/testdata/twitter/tweet_text_only.html — the
	// shape ExtractTweet actually parses. statusID 9999999999999999999 must
	// match the URL below so IsTwitterStatusURL hands it through.
	twitterHTML := `<!DOCTYPE html>
<html><head><title>karpathy on X</title></head><body>
<main>
  <article role="article" data-testid="tweet" tabindex="-1">
    <div data-testid="User-Name">
      <a role="link" href="/karpathy"><span>Andrej Karpathy</span></a>
      <a role="link" href="/karpathy"><span>@karpathy</span></a>
    </div>
    <div data-testid="tweetText">
      <span>hello from twitter.</span>
    </div>
    <a href="/karpathy/status/9999999999999999999" role="link"><time datetime="2026-04-21T09:00:00.000Z">Apr 21</time></a>
  </article>
</main>
</body></html>`

	plainHTML := `<html><body><article>
		<h1>A normal article title</h1>
		<p>Body paragraph one with enough characters to pass the 200-char gate easily right here for sure.</p>
		<p>Body paragraph two with enough characters to pass the 200-char gate easily right here for sure.</p>
	</article></body></html>`

	cases := []struct {
		name     string
		url      string
		html     string
		wantKind string
	}{
		{"twitter status url → tweet", "https://x.com/karpathy/status/9999999999999999999", twitterHTML, "tweet"},
		{"regular article url → article", "https://example.com/blog/post-1", plainHTML, "article"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newTestBookmarkletHandlerForPDF(t)
			r := gin.New()
			r.POST("/api/bookmarklet/capture", h.Capture)

			body, _ := json.Marshal(map[string]any{
				"url":   tc.url,
				"title": "ignored — handler reassigns from extractor",
				"html":  tc.html,
			})
			req := httptest.NewRequest("POST", "/api/bookmarklet/capture", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
			}
			if len(repo.created) != 1 {
				t.Fatalf("expected 1 created article, got %d", len(repo.created))
			}
			art := repo.created[0]
			if art.Kind != tc.wantKind {
				t.Errorf("article.Kind = %q, want %q", art.Kind, tc.wantKind)
			}
		})
	}
}

func TestCapturePersistsAudioFromCapturedHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture", h.Capture)

	const mediaURL = "https://8.8.8.8/media/6bd5f8e7-6e06-4788-99f0-5d6604020e57.mp3"
	html := `<html><body>
		<audio src="https://cdn.example.com/navigation-preview.mp3"></audio>
		<span class="user-html">
		<p><audio src="/media/6bd5f8e7-6e06-4788-99f0-5d6604020e57.mp3" controls></audio></p>
		<p>音频文章正文。这一段刻意写得足够长，以便文章正文选择器直接命中当前节点，并覆盖真实音频网摘的抓取行为。</p>
		<p>第二段继续补充正文内容，确保整个测试样本超过长度阈值，同时不会依赖页面外层的分享区或推荐区。</p>
	</span></body></html>`
	body, _ := json.Marshal(map[string]any{
		"url":   "https://8.8.8.8/careers/audio-article",
		"title": "音频文章",
		"html":  html,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/bookmarklet/capture", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("created articles = %d, want 1", len(repo.created))
	}
	article := repo.created[0]
	if article.MediaURL != mediaURL || article.MediaType != "audio/mpeg" {
		t.Fatalf("created media = (%q, %q), want (%q, audio/mpeg)", article.MediaURL, article.MediaType, mediaURL)
	}
}

func TestCaptureBackfillsAudioWhenRecapturingExistingArticle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture", h.Capture)

	const articleURL = "https://8.8.8.8/careers/audio-article"
	const mediaURL = "https://cdn.linkresearcher.com/6bd5f8e7-6e06-4788-99f0-5d6604020e57.mp3"
	repo.byOwnerAndURL["42|"+articleURL] = &model.Article{
		ID:      2692,
		FeedID:  7,
		URL:     articleURL,
		Title:   "旧标题",
		Content: "旧正文",
	}
	html := `<html><body><span class="user-html">
		<p><audio src="` + mediaURL + `" controls></audio></p>
		<p>重新抓取后的音频文章正文。这一段刻意写得足够长，以便文章正文选择器直接命中当前节点并完成更新。</p>
		<p>第二段继续补充正文内容，确保测试覆盖现有文章重新抓取时的媒体信息回填。</p>
	</span></body></html>`
	body, _ := json.Marshal(map[string]any{
		"url":   articleURL,
		"title": "新标题",
		"html":  html,
		"force": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/bookmarklet/capture", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.mediaArticleID != 2692 || repo.mediaURL != mediaURL || repo.mediaType != "audio/mpeg" {
		t.Fatalf("media refresh = article=%d media=(%q, %q)", repo.mediaArticleID, repo.mediaURL, repo.mediaType)
	}
}

func TestCaptureClearsStaleAudioWhenRecaptureHasNoMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture", h.Capture)

	const articleURL = "https://8.8.8.8/careers/text-article"
	repo.byOwnerAndURL["42|"+articleURL] = &model.Article{
		ID:        2692,
		FeedID:    7,
		URL:       articleURL,
		Title:     "旧标题",
		Content:   "旧正文",
		MediaURL:  "https://cdn.example.com/obsolete-episode.mp3",
		MediaType: "audio/mpeg",
	}
	html := `<html><body><span class="user-html">
		<p>重新抓取后的普通文章正文。这一段刻意写得足够长，以便文章正文选择器直接命中当前节点并完成更新。</p>
		<p>第二段继续补充正文内容，但页面中已经没有任何音频元素，旧播放器应当随显式重抓一起移除。</p>
	</span></body></html>`
	body, _ := json.Marshal(map[string]any{
		"url":   articleURL,
		"title": "新标题",
		"html":  html,
		"force": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/bookmarklet/capture", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.mediaArticleID != 2692 || repo.mediaURL != "" || repo.mediaType != "" {
		t.Fatalf("media clear = article=%d media=(%q, %q)", repo.mediaArticleID, repo.mediaURL, repo.mediaType)
	}
}

func TestCaptureDoesNotClassifyUnsafeArticleURLAsMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, repo := newTestBookmarkletHandlerForPDF(t)
	r := gin.New()
	r.POST("/api/bookmarklet/capture", h.Capture)

	html := `<html><body><span class="user-html">
		<p><audio src="https://cdn.example.com/public-episode.mp3" controls></audio></p>
		<p>正文内容足够长，以便正文选择器命中，但文章地址本身指向 loopback，不能让媒体字段触发后台网络抓取。</p>
		<p>第二段继续补充正文，确保这个测试只验证媒体分类的 SSRF 边界。</p>
	</span></body></html>`
	body, _ := json.Marshal(map[string]any{
		"url":   "http://127.0.0.1/private-audio",
		"title": "不安全地址",
		"html":  html,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/bookmarklet/capture", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("created articles = %d, want 1", len(repo.created))
	}
	if repo.created[0].MediaURL != "" || repo.created[0].MediaType != "" {
		t.Fatalf("unsafe article persisted media=(%q, %q)", repo.created[0].MediaURL, repo.created[0].MediaType)
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
