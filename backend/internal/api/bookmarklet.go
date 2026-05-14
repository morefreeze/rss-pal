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
	"regexp"
	"strings"

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
	content, err := extractContentFromHTML(req.HTML, req.URL)
	if err != nil || strings.TrimSpace(content) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
		return
	}

	title := strings.TrimSpace(req.Title)
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
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d (force=%v)", existing.ID, user.ID, normalized, newLen, req.Force)
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + existing.Title,
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
		FeedID:  feed.ID,
		Title:   title,
		URL:     normalized,
		Content: content,
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
	rss.ResolveURLs(doc, baseURL)

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

// GenerateBookmarkletToken returns a 32-byte random hex string suitable for
// users.bookmarklet_token. Used by the Settings regenerate endpoint.
func GenerateBookmarkletToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
