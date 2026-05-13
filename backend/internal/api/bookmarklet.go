package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/gin-gonic/gin"
)

// captureMaxBodyBytes caps the JSON body the bookmarklet can send. 4 MiB
// accommodates script/style-heavy article pages (e.g. mp.weixin.qq.com,
// which routinely ships ~3 MiB of inline JS/CSS) even before the bookmarklet's
// client-side trim runs; abusive payloads above this cap produce a 413.
const captureMaxBodyBytes = 4 << 20 // 4 MiB

// duplicateOverwriteRatio is the threshold at which a re-captured article's
// new content is considered a clear improvement and we silently overwrite.
// Below this ratio, the receiver page asks the user to confirm.
const duplicateOverwriteRatio = 1.5

// markdownImageRe matches markdown image syntax: ![alt](url). Used to count
// images for the duplicate-prompt comparison so users see at a glance when
// a re-capture lost images (typical login-wall regression).
var markdownImageRe = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)

func countMarkdownImages(s string) int {
	return len(markdownImageRe.FindAllStringIndex(s, -1))
}

// shouldPromptDuplicate returns true when a bookmarklet capture for an
// existing URL should pause and ask the user (rather than auto-overwriting).
// Pure function so it can be unit-tested without a DB. Triggers a prompt on:
//   - length regression: new content is below 1.5x the old length, or
//   - image regression: new content has strictly fewer markdown images.
// force=true bypasses everything (used after the user explicitly chose
// to overwrite).
func shouldPromptDuplicate(newLen, oldLen, newImages, oldImages int, force bool) bool {
	if force {
		return false
	}
	if oldLen == 0 {
		return false
	}
	if newImages < oldImages {
		return true
	}
	return float64(newLen) < duplicateOverwriteRatio*float64(oldLen)
}

type BookmarkletHandler struct {
	userRepo    *repository.UserRepository
	feedRepo    *repository.FeedRepository
	articleRepo *repository.ArticleRepository
}

func NewBookmarkletHandler(
	userRepo *repository.UserRepository,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *BookmarkletHandler {
	return &BookmarkletHandler{
		userRepo:    userRepo,
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
	}
}

// Capture is the POST /api/bookmarklet/capture handler. It does its own
// bearer-token authentication against users.bookmarklet_token (no JWT) so it
// can be invoked from any third-party origin.
func (h *BookmarkletHandler) Capture(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 bookmarklet token"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captureMaxBodyBytes)
	var req struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		HTML  string `json:"html"`
		Force bool   `json:"force"`
	}
	dec := json.NewDecoder(c.Request.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "内容过大"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.URL == "" || req.HTML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 和 html 必填"})
		return
	}

	normalized := util.NormalizeURL(req.URL)

	var (
		content     string
		title       = strings.TrimSpace(req.Title)
		publishedAt *time.Time
		wasTwitter  bool
	)

	if statusID, ok := rss.IsTwitterStatusURL(normalized); ok {
		cap, err := rss.ExtractTweet(req.HTML, statusID)
		if err == nil {
			content = buildTweetContent(cap)
			title = buildTweetTitle(cap)
			if !cap.PublishedAt.IsZero() {
				t := cap.PublishedAt
				publishedAt = &t
			}
			wasTwitter = true
		} else {
			log.Printf("bookmarklet: twitter extract for %s failed (%v); falling back to generic extractor", normalized, err)
		}
	}

	if !wasTwitter {
		body, err := extractContentFromHTML(req.HTML, req.URL)
		if err != nil || strings.TrimSpace(body) == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
			return
		}
		content = body
	}

	if title == "" {
		title = normalized
	}

	existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
	if err != nil {
		log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
		return
	}

	if existing != nil {
		newLen, oldLen := len(content), len(existing.Content)
		newImages := countMarkdownImages(content)
		oldImages := countMarkdownImages(existing.Content)
		if shouldPromptDuplicate(newLen, oldLen, newImages, oldImages, req.Force) {
			c.JSON(http.StatusOK, gin.H{
				"status":          "duplicate",
				"article_id":      existing.ID,
				"existing_length": oldLen,
				"new_length":      newLen,
				"existing_images": oldImages,
				"new_images":      newImages,
				"message":         fmt.Sprintf("已有内容 %d 字 %d 图 / 新内容 %d 字 %d 图", oldLen, oldImages, newLen, newImages),
			})
			return
		}
		wc, rm := rss.ComputeMetrics(content)
		if err := h.articleRepo.UpdateContent(existing.ID, content, wc, rm); err != nil {
			log.Printf("bookmarklet: UpdateContent failed for article=%d: %v", existing.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新文章失败"})
			return
		}
		// A bookmarklet re-capture is the user explicitly asking for a fresh
		// parse — refresh the title too, otherwise stale titles from a
		// previous (worse) extraction stick around forever.
		if title != "" && title != existing.Title {
			if err := h.articleRepo.UpdateTitle(existing.ID, title); err != nil {
				log.Printf("bookmarklet: UpdateTitle failed for article=%d: %v", existing.ID, err)
			}
		}
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d (force=%v)", existing.ID, user.ID, normalized, newLen, req.Force)
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + title,
		})
		return
	}

	feed, err := h.feedRepo.GetOrCreateSavedFeed(user.ID)
	if err != nil {
		log.Printf("bookmarklet: GetOrCreateSavedFeed failed for user=%d: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建网摘 feed 失败"})
		return
	}

	article := &model.Article{
		FeedID:      feed.ID,
		Title:       title,
		URL:         normalized,
		Content:     content,
		PublishedAt: publishedAt, // nil for non-twitter (preserves existing behavior)
	}
	article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
	if err := h.articleRepo.Create(article); err != nil {
		log.Printf("bookmarklet: Create article failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "新建文章失败"})
		return
	}
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"article_id": article.ID,
		"message":    "已加入网摘: " + title,
	})
}

func (h *BookmarkletHandler) authenticate(c *gin.Context) (*model.User, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing token")
	}
	token := authHeader
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = authHeader[7:]
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("empty token")
	}
	user, err := h.userRepo.GetByBookmarkletToken(token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("token not found")
	}
	return user, nil
}

// extractContentFromHTML parses the captured outerHTML through goquery and
// pulls out the largest body of content it can find as Markdown, using the
// same selector strategy as internal/rss/content.go::fetchDirect. Image and
// link URLs are resolved against baseURL so images render correctly when the
// source page used relative or protocol-relative paths (typical for SPAs).
func extractContentFromHTML(html, baseURL string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	// Cleanup must not strip top-level containers even if their class happens
	// to match an attribute-substring selector. WeChat sets
	// <body class="… comment_feature …"> which would otherwise be wiped by
	// [class*=comment], leaving the document empty.
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Not("html, body, head, main, article").Remove()
	rss.StripAvatars(doc)
	rss.PromoteLazyImages(doc)

	resolveURLs(doc, baseURL)

	var content string
	candidates := []string{
		"article", "[role='main']", "main",
		".post-content", ".article-content", ".article-body", ".entry-content",
		".story-body", ".post-body", ".field-item",
		".article-text", ".article__body", ".content-article",
		"[class*=article-detail]", "[class*=articleDetail]", "[class*=post-detail]",
		"[id*=article-body]", "[id*=articleBody]", "[id*=js_content]",
		".content", ".post", "#content", "#main", "body",
	}
	for _, sel := range candidates {
		nodes := doc.Find(sel)
		if nodes.Length() == 0 {
			continue
		}
		c := rss.ExtractMarkdown(nodes.First())
		if len(c) > len(content) {
			content = c
		}
		if len(content) > 200 {
			break
		}
	}

	if strings.TrimSpace(content) == "" {
		var b strings.Builder
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			md := rss.ExtractMarkdown(s)
			if len(md) > 30 {
				b.WriteString(md)
				b.WriteString("\n\n")
			}
		})
		content = b.String()
	}

	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	return strings.TrimSpace(content), nil
}

// resolveURLs rewrites relative img[src] and a[href] attributes to absolute
// URLs against baseURL. Bookmarklet captures send the source page's
// outerHTML, which often contains site-relative ("/foo.jpg"),
// protocol-relative ("//cdn/foo.jpg"), or path-relative ("foo.jpg") URLs —
// these would otherwise render as broken links once the article is viewed
// on the RSS Pal host. data: URIs are preserved as-is.
func resolveURLs(doc *goquery.Document, baseURL string) {
	base, err := url.Parse(baseURL)
	if err != nil || !base.IsAbs() {
		return
	}
	rewrite := func(s *goquery.Selection, attr string) {
		raw, ok := s.Attr(attr)
		if !ok {
			return
		}
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "data:") {
			return
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		s.SetAttr(attr, base.ResolveReference(ref).String())
	}
	doc.Find("img[src]").Each(func(_ int, s *goquery.Selection) { rewrite(s, "src") })
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) { rewrite(s, "href") })
}

// GenerateBookmarkletToken returns a 32-byte random hex string suitable for
// users.bookmarklet_token. Used by the Settings regenerate endpoint.
func GenerateBookmarkletToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// buildTweetContent renders a TweetCapture as the article body. The first
// line is a markdown blockquote that carries the author byline and date,
// which the reader renders just like Twitter's own attribution row. Empty
// fields are silently dropped — image-only and quote-only tweets still
// produce a useful article body.
func buildTweetContent(cap *rss.TweetCapture) string {
	var sections []string

	if byline := buildTweetByline(cap); byline != "" {
		sections = append(sections, byline)
	}
	if cap.ArticleTitle != "" {
		sections = append(sections, "# "+cap.ArticleTitle)
	}
	if cap.TextMarkdown != "" {
		sections = append(sections, cap.TextMarkdown)
	}
	for _, img := range cap.ImageURLs {
		sections = append(sections, "![]("+img+")")
	}
	if quote := buildQuoteSection(cap.Quote); quote != "" {
		sections = append(sections, quote)
	}

	return strings.Join(sections, "\n\n")
}

// buildQuoteSection renders a Quote as a markdown block: an "引用" header
// line linking to the source (with @handle (DisplayName) · YYYY-MM-DD when
// available), followed by a blockquote of the excerpt. Degrades gracefully:
// a quote with only a URL falls back to the bare "引用: <url>" line so we
// don't lose the link.
func buildQuoteSection(q *rss.Quote) string {
	if q == nil || q.URL == "" {
		return ""
	}
	if q.Author == "" && q.Excerpt == "" {
		return "引用: " + q.URL
	}

	var b strings.Builder
	b.WriteString("**引用** ")
	if q.Author != "" {
		b.WriteString("[@")
		b.WriteString(q.Author)
		if q.DisplayName != "" {
			b.WriteString(" (")
			b.WriteString(q.DisplayName)
			b.WriteString(")")
		}
		b.WriteString("](")
		b.WriteString(q.URL)
		b.WriteString(")")
	} else {
		b.WriteString("[")
		b.WriteString(q.URL)
		b.WriteString("](")
		b.WriteString(q.URL)
		b.WriteString(")")
	}
	if !q.PublishedAt.IsZero() {
		b.WriteString(" · ")
		b.WriteString(q.PublishedAt.UTC().Format("2006-01-02"))
	}

	if q.Excerpt != "" {
		b.WriteString("\n\n")
		for i, line := range strings.Split(q.Excerpt, "\n") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("> ")
			b.WriteString(line)
		}
	}
	return b.String()
}

// buildTweetByline produces "> @handle (DisplayName) · YYYY-MM-DD" when
// possible, degrading gracefully when any component is missing. Returns ""
// only when even the handle is missing (extremely rare — we got the
// statusID from a URL that also contained the handle, so this would mean
// the parser couldn't find any profile anchor inside the focal article).
func buildTweetByline(cap *rss.TweetCapture) string {
	if cap.Author == "" && cap.DisplayName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("> ")
	if cap.Author != "" {
		b.WriteString("@")
		b.WriteString(cap.Author)
	}
	if cap.DisplayName != "" {
		if cap.Author != "" {
			b.WriteString(" ")
		}
		b.WriteString("(")
		b.WriteString(cap.DisplayName)
		b.WriteString(")")
	}
	if !cap.PublishedAt.IsZero() {
		b.WriteString(" · ")
		b.WriteString(cap.PublishedAt.UTC().Format("2006-01-02"))
	}
	return b.String()
}

// buildTweetTitle renders a feed-list-friendly tweet title in the form
// "<DisplayName> · <first clause>". The first clause is the text up to the
// first sentence/clause break (.!?。！？,，) that falls within a useful
// rune-count range; absent a break we walk back to the last word boundary
// within 60 runes. Empty text falls back to "@handle 的推文" for
// image-only tweets. Final fallback is "Twitter 推文".
func buildTweetTitle(cap *rss.TweetCapture) string {
	// X Articles have their own title — use it verbatim (with a name prefix
	// when available) so the feed shows the article's real heading rather
	// than the first clause of its lead paragraph.
	if cap.ArticleTitle != "" {
		switch {
		case cap.DisplayName != "":
			return cap.DisplayName + " · " + cap.ArticleTitle
		case cap.Author != "":
			return "@" + cap.Author + " · " + cap.ArticleTitle
		default:
			return cap.ArticleTitle
		}
	}

	text := strings.TrimSpace(cap.TextMarkdown)
	if text == "" {
		if cap.DisplayName != "" {
			return cap.DisplayName + " 的推文"
		}
		if cap.Author != "" {
			return "@" + cap.Author + " 的推文"
		}
		return "Twitter 推文"
	}

	body := firstClauseOrTruncate(strings.ReplaceAll(text, "\n", " "))

	switch {
	case cap.DisplayName != "":
		return cap.DisplayName + " · " + body
	case cap.Author != "":
		return "@" + cap.Author + " · " + body
	default:
		return body
	}
}

// firstClauseOrTruncate returns text up to the first clause break
// (.!?。！？,，) whose rune index is in [8, 80] — short enough to read at a
// glance, long enough to not collapse on "+1!" or "lol". When no useful
// break exists, falls back to a 60-rune cap snapped to the last space in
// the upper half of the window (so we don't slice mid-word for English).
func firstClauseOrTruncate(text string) string {
	const minClause, maxClause = 8, 80
	runes := []rune(text)
	breakIdx := -1
	for i, r := range runes {
		if i < minClause {
			continue
		}
		if i > maxClause {
			break
		}
		switch r {
		case '.', '!', '?', '。', '！', '？', ',', '，':
			breakIdx = i
		}
		if breakIdx != -1 {
			break
		}
	}
	if breakIdx != -1 {
		return strings.TrimRight(string(runes[:breakIdx]), " ")
	}
	if len(runes) <= 60 {
		return text
	}
	chunk := string(runes[:60])
	if lastSpace := strings.LastIndex(chunk, " "); lastSpace > 30 {
		return strings.TrimRight(chunk[:lastSpace], " ") + "…"
	}
	return chunk + "…"
}
