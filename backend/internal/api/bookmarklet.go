package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
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

// articleKind returns the model.Article.Kind value to set on a freshly
// captured article. Twitter / X captures get "tweet" so the frontend can
// render them with the TweetCard component; everything else gets the
// default "article".
func articleKind(wasTwitter bool) string {
	if wasTwitter {
		return "tweet"
	}
	return "article"
}

// shouldPromptDuplicate returns true when a bookmarklet capture for an
// existing URL should pause and ask the user (rather than auto-overwriting).
// Pure function so it can be unit-tested without a DB. Triggers a prompt on:
//   - length regression: new content is below 1.5x the old length, or
//   - image regression: new content has strictly fewer markdown images.
//
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

// bookmarkletUserRepo is the subset of *repository.UserRepository the
// bookmarklet handlers need. Defined as an interface so tests can swap in
// a stub without standing up a real database. Concrete repository pointers
// satisfy it via Go's structural typing.
//
// WithCtx returns a tx-bound view; today a no-op for public-token handlers
// (no tx in context), kept on the interface so handler call sites stay
// uniform with the rest of the codebase and Phase 4.2 can wrap them in a
// tx without any further handler edits.
type bookmarkletUserRepo interface {
	GetByBookmarkletToken(token string) (*model.User, error)
	WithCtx(c ctxkey.CtxGetter) bookmarkletUserRepo
}

// bookmarkletFeedRepo is the subset of *repository.FeedRepository the
// bookmarklet handlers need.
type bookmarkletFeedRepo interface {
	GetOrCreateClipFeed(ownerID int) (*model.Feed, error)
	WithCtx(c ctxkey.CtxGetter) bookmarkletFeedRepo
}

// bookmarkletArticleRepo is the subset of *repository.ArticleRepository
// the bookmarklet handlers need. Covers both the existing HTML capture
// flow and the new PDF capture flow.
type bookmarkletArticleRepo interface {
	FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error)
	Create(article *model.Article) error
	UpdateContent(id int, content string, wordCount, readingMinutes int) error
	UpdateTitle(id int, title string) error
	UpdateSummary(id int, summaryBrief, summaryDetailed string) error
	// PDF-specific (added for capture-pdf / capture-pdf-url):
	CreatePDFStub(a *model.Article) error
	UpdateContentAndMarkReady(id int, content string, wordCount, readingMinutes int) error
	MarkPDFFailed(id int, msg string) error
	ResetPDFToProcessing(id int) error
	WithCtx(c ctxkey.CtxGetter) bookmarkletArticleRepo
}

type BookmarkletHandler struct {
	userRepo     bookmarkletUserRepo
	feedRepo     bookmarkletFeedRepo
	articleRepo  bookmarkletArticleRepo
	backup       *backup.Runner // nil when backup is disabled
	imageBaseDir string         // root for pdfextract image storage; "" until WithImageBaseDir
}

func NewBookmarkletHandler(
	userRepo *repository.UserRepository,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *BookmarkletHandler {
	return &BookmarkletHandler{
		userRepo:    bookmarkletUserRepoAdapter{userRepo},
		feedRepo:    bookmarkletFeedRepoAdapter{feedRepo},
		articleRepo: bookmarkletArticleRepoAdapter{articleRepo},
	}
}

// Adapter shims wrap the concrete repositories so their WithCtx methods
// return the interface type (rather than *repository.XRepository). The
// stubs in *_test.go implement the interface directly, so they are not
// wrapped.

type bookmarkletUserRepoAdapter struct{ r *repository.UserRepository }

func (a bookmarkletUserRepoAdapter) GetByBookmarkletToken(token string) (*model.User, error) {
	return a.r.GetByBookmarkletToken(token)
}
func (a bookmarkletUserRepoAdapter) WithCtx(c ctxkey.CtxGetter) bookmarkletUserRepo {
	return bookmarkletUserRepoAdapter{a.r.WithCtx(c)}
}

type bookmarkletFeedRepoAdapter struct{ r *repository.FeedRepository }

func (a bookmarkletFeedRepoAdapter) GetOrCreateClipFeed(ownerID int) (*model.Feed, error) {
	return a.r.GetOrCreateClipFeed(ownerID)
}
func (a bookmarkletFeedRepoAdapter) WithCtx(c ctxkey.CtxGetter) bookmarkletFeedRepo {
	return bookmarkletFeedRepoAdapter{a.r.WithCtx(c)}
}

type bookmarkletArticleRepoAdapter struct{ r *repository.ArticleRepository }

func (a bookmarkletArticleRepoAdapter) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	return a.r.FindByOwnerAndURL(ownerID, exactURL)
}
func (a bookmarkletArticleRepoAdapter) Create(article *model.Article) error {
	return a.r.Create(article)
}
func (a bookmarkletArticleRepoAdapter) UpdateContent(id int, content string, wordCount, readingMinutes int) error {
	return a.r.UpdateContent(id, content, wordCount, readingMinutes)
}
func (a bookmarkletArticleRepoAdapter) UpdateTitle(id int, title string) error {
	return a.r.UpdateTitle(id, title)
}
func (a bookmarkletArticleRepoAdapter) UpdateSummary(id int, summaryBrief, summaryDetailed string) error {
	return a.r.UpdateSummary(id, summaryBrief, summaryDetailed)
}
func (a bookmarkletArticleRepoAdapter) CreatePDFStub(art *model.Article) error {
	return a.r.CreatePDFStub(art)
}
func (a bookmarkletArticleRepoAdapter) UpdateContentAndMarkReady(id int, content string, wordCount, readingMinutes int) error {
	return a.r.UpdateContentAndMarkReady(id, content, wordCount, readingMinutes)
}
func (a bookmarkletArticleRepoAdapter) MarkPDFFailed(id int, msg string) error {
	return a.r.MarkPDFFailed(id, msg)
}
func (a bookmarkletArticleRepoAdapter) ResetPDFToProcessing(id int) error {
	return a.r.ResetPDFToProcessing(id)
}
func (a bookmarkletArticleRepoAdapter) WithCtx(c ctxkey.CtxGetter) bookmarkletArticleRepo {
	return bookmarkletArticleRepoAdapter{a.r.WithCtx(c)}
}

// WithBackupRunner wires a backup runner so successful captures trigger a
// debounced snapshot. Pass nil to disable.
func (h *BookmarkletHandler) WithBackupRunner(r *backup.Runner) *BookmarkletHandler {
	h.backup = r
	return h
}

// WithImageBaseDir sets the root directory under which PDF clip image
// assets are stored (one subdir per article). Falls back to a no-op image
// storage path when unset (writes still happen but the dir is "").
func (h *BookmarkletHandler) WithImageBaseDir(path string) *BookmarkletHandler {
	h.imageBaseDir = path
	return h
}

// ResolveOwner implements PublicTokenResolver for the bookmarklet
// endpoints. It reads the Authorization: Bearer token directly off the
// request and resolves it against users.bookmarklet_token via the open
// tx. The users table is intentionally NOT RLS-protected, so this lookup
// works before app.user_id has been set on the tx.
//
// Missing/empty/unknown tokens map to ErrPublicTokenInvalid → 401.
func (h *BookmarkletHandler) ResolveOwner(c *gin.Context, tx *sql.Tx) (int, error) {
	token := bearerToken(c)
	if token == "" {
		return 0, ErrPublicTokenInvalid
	}
	var uid int
	err := tx.QueryRow(`SELECT id FROM users WHERE bookmarklet_token = $1`, token).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPublicTokenInvalid
	}
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// Capture is the POST /api/bookmarklet/capture handler. It does its own
// bearer-token authentication against users.bookmarklet_token (no JWT) so it
// can be invoked from any third-party origin. When wired via
// PublicTokenMiddleware the middleware ALSO performs the lookup (to set
// app.user_id on the tx) — the handler re-authenticates here to keep the
// flow symmetric with extension/share and so that direct unit tests of the
// handler still work without standing up the middleware.
func (h *BookmarkletHandler) Capture(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 bookmarklet token"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captureMaxBodyBytes)
	var req struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		HTML     string `json:"html"`
		Force    bool   `json:"force"`
		ForceNew bool   `json:"force_new"`
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

	normalized := util.NormalizeURLKeepFragment(req.URL)

	var (
		content     string
		title       = strings.TrimSpace(req.Title)
		publishedAt *time.Time
		wasTwitter  bool
	)

	if statusID, ok := rss.IsTwitterStatusURL(normalized); ok {
		cap, err := rss.ExtractTweet(req.HTML, statusID)
		if err == nil {
			content = rss.BuildTweetContent(cap)
			title = rss.BuildTweetTitle(cap)
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

	articleRepo := h.articleRepo.WithCtx(c)
	var existing *model.Article
	if !req.ForceNew {
		var err error
		existing, err = articleRepo.FindByOwnerAndURL(user.ID, normalized)
		if err != nil {
			log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
			return
		}
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
		if err := articleRepo.UpdateContent(existing.ID, content, wc, rm); err != nil {
			log.Printf("bookmarklet: UpdateContent failed for article=%d: %v", existing.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新文章失败"})
			return
		}
		// A bookmarklet re-capture is the user explicitly asking for a fresh
		// parse — refresh the title too, otherwise stale titles from a
		// previous (worse) extraction stick around forever.
		if title != "" && title != existing.Title {
			if err := articleRepo.UpdateTitle(existing.ID, title); err != nil {
				log.Printf("bookmarklet: UpdateTitle failed for article=%d: %v", existing.ID, err)
			}
		}
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d (force=%v)", existing.ID, user.ID, normalized, newLen, req.Force)
		if h.backup != nil {
			h.backup.TriggerAsync()
		}
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + title,
		})
		return
	}

	feed, err := h.feedRepo.WithCtx(c).GetOrCreateClipFeed(user.ID)
	if err != nil {
		log.Printf("bookmarklet: GetOrCreateClipFeed failed for user=%d: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建网摘 feed 失败"})
		return
	}

	article := &model.Article{
		FeedID:      feed.ID,
		Title:       title,
		URL:         normalized,
		Content:     content,
		PublishedAt: publishedAt, // tweet's original time for Twitter captures, nil otherwise
		IsClip:      true,
		Kind:        articleKind(wasTwitter),
	}
	article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
	if err := articleRepo.Create(article); err != nil {
		log.Printf("bookmarklet: Create article failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "新建文章失败"})
		return
	}
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	if h.backup != nil {
		h.backup.TriggerAsync()
	}
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
	user, err := h.userRepo.WithCtx(c).GetByBookmarkletToken(token)
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
	// If the HTML is a clean extraction from our extension (body contains
	// #js_content and optionally #wx_images), skip the aggressive generic
	// cleanup — the extension already did targeted cleanup and the generic
	// selectors (e.g. [class*=share], [class*=comment]) would strip WeChat
	// article content.
	isCleanExtraction := doc.Find("body > #js_content").Length() == 1

	if !isCleanExtraction {
		doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Not("html, body, head, main, article").Remove()
	} else {
		doc.Find("script, style").Remove()
	}

	rss.StripAvatars(doc)
	rss.PromoteLazyImages(doc)
	rss.ResolveURLs(doc, baseURL)

	var content string
	candidates := []string{
		// WeChat: #js_content is the authoritative content container; check first
		// so the 200-char early-break doesn't settle on a noisier candidate.
		"#js_content",
		"article", "[role='main']", "main",
		".post-content", ".article-content", ".article-body", ".entry-content",
		".story-body", ".post-body", ".field-item",
		".article-text", ".article__body", ".content-article",
		"[class*=article-detail]", "[class*=articleDetail]", "[class*=post-detail]",
		"[id*=article-body]", "[id*=articleBody]",
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

// GenerateBookmarkletToken returns a 32-byte random hex string suitable for
// users.bookmarklet_token. Used by the Settings regenerate endpoint.
func GenerateBookmarkletToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

