package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/gin-gonic/gin"
)

// captureMaxBodyBytes caps the body the bookmarklet can send. 1 MiB is
// generous for outerHTML on a typical article page; abusive payloads are
// truncated and produce a 413.
const captureMaxBodyBytes = 1 << 20 // 1 MiB

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

type captureInput struct {
	Token string
	URL   string
	Title string
	HTML  string
}

type captureResult struct {
	Status    int    // HTTP status code
	Phase     string // "created" | "updated" | "unchanged" | "error"
	Message   string // user-facing message
	ArticleID int
}

// Capture is the POST /api/bookmarklet/capture handler. It auto-detects
// JSON vs form-encoded input — the form path exists so the bookmarklet can
// dodge strict `connect-src` CSP on third-party sites (form submission is
// governed by `form-action`, not `connect-src`). JSON keeps the API
// programmable.
func (h *BookmarkletHandler) Capture(c *gin.Context) {
	contentType := c.ContentType()
	formMode := strings.HasPrefix(contentType, "multipart/form-data") ||
		strings.HasPrefix(contentType, "application/x-www-form-urlencoded")

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captureMaxBodyBytes)

	input, parseErr, tooBig := parseCaptureRequest(c, formMode)
	if parseErr != nil {
		if tooBig {
			respondCapture(c, formMode, captureResult{Status: http.StatusRequestEntityTooLarge, Phase: "error", Message: "内容过大"})
			return
		}
		respondCapture(c, formMode, captureResult{Status: http.StatusBadRequest, Phase: "error", Message: "请求格式错误"})
		return
	}

	if input.URL == "" || input.HTML == "" {
		respondCapture(c, formMode, captureResult{Status: http.StatusBadRequest, Phase: "error", Message: "url 和 html 必填"})
		return
	}

	user, err := h.authenticate(input.Token)
	if err != nil {
		respondCapture(c, formMode, captureResult{Status: http.StatusUnauthorized, Phase: "error", Message: "无效的 bookmarklet token"})
		return
	}

	respondCapture(c, formMode, h.processCapture(user, input))
}

func parseCaptureRequest(c *gin.Context, formMode bool) (captureInput, error, bool) {
	if formMode {
		// ParseMultipartForm handles both multipart and urlencoded.
		if err := c.Request.ParseMultipartForm(captureMaxBodyBytes); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return captureInput{}, err, true
			}
			// Non-multipart bodies fall through here; ParseForm reads urlencoded.
			if perr := c.Request.ParseForm(); perr != nil {
				if errors.As(perr, &maxErr) {
					return captureInput{}, perr, true
				}
				return captureInput{}, perr, false
			}
		}
		return captureInput{
			Token: c.PostForm("token"),
			URL:   c.PostForm("url"),
			Title: c.PostForm("title"),
			HTML:  c.PostForm("html"),
		}, nil, false
	}

	var req struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return captureInput{}, err, true
		}
		return captureInput{}, err, false
	}
	authHeader := c.GetHeader("Authorization")
	token := authHeader
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = authHeader[7:]
	}
	return captureInput{
		Token: strings.TrimSpace(token),
		URL:   req.URL,
		Title: req.Title,
		HTML:  req.HTML,
	}, nil, false
}

func (h *BookmarkletHandler) processCapture(user *model.User, input captureInput) captureResult {
	normalized := util.NormalizeURL(input.URL)
	content, err := extractContentFromHTML(input.HTML)
	if err != nil || strings.TrimSpace(content) == "" {
		return captureResult{Status: http.StatusUnprocessableEntity, Phase: "error", Message: "无法从页面提取正文"}
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = normalized
	}

	existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
	if err != nil {
		log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
		return captureResult{Status: http.StatusInternalServerError, Phase: "error", Message: "查询文章失败"}
	}

	if existing != nil {
		if len(content) <= len(existing.Content) {
			return captureResult{Status: http.StatusOK, Phase: "unchanged", ArticleID: existing.ID, Message: "已有内容更完整,未覆盖"}
		}
		wc, rm := rss.ComputeMetrics(content)
		if err := h.articleRepo.UpdateContent(existing.ID, content, wc, rm); err != nil {
			log.Printf("bookmarklet: UpdateContent failed for article=%d: %v", existing.ID, err)
			return captureResult{Status: http.StatusInternalServerError, Phase: "error", Message: "更新文章失败"}
		}
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d", existing.ID, user.ID, normalized, len(content))
		return captureResult{Status: http.StatusOK, Phase: "updated", ArticleID: existing.ID, Message: "已更新文章: " + existing.Title}
	}

	feed, err := h.feedRepo.GetOrCreateSavedFeed(user.ID)
	if err != nil {
		log.Printf("bookmarklet: GetOrCreateSavedFeed failed for user=%d: %v", user.ID, err)
		return captureResult{Status: http.StatusInternalServerError, Phase: "error", Message: "创建收藏 feed 失败"}
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
		return captureResult{Status: http.StatusInternalServerError, Phase: "error", Message: "新建文章失败"}
	}
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	return captureResult{Status: http.StatusCreated, Phase: "created", ArticleID: article.ID, Message: "已收藏: " + title}
}

func respondCapture(c *gin.Context, formMode bool, r captureResult) {
	if !formMode {
		var body gin.H
		if r.Phase == "error" {
			body = gin.H{"error": r.Message}
		} else {
			body = gin.H{"status": r.Phase, "message": r.Message}
			if r.ArticleID != 0 {
				body["article_id"] = r.ArticleID
			}
		}
		c.JSON(r.Status, body)
		return
	}
	renderCaptureHTML(c, r)
}

// captureResultPage is rendered into the new tab opened by the bookmarklet.
// Auto-closes after 2.5s on success; stays open on error so the user can
// read the message.
var captureResultPage = template.Must(template.New("capture").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>RSS Pal - {{.Heading}}</title>
<style>
body{font:16px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f9fafb;color:#222}
.box{padding:28px 36px;background:#fff;border-radius:14px;box-shadow:0 6px 24px rgba(0,0,0,.08);max-width:520px;text-align:center}
h1{margin:0 0 12px;font-size:20px}
h1.ok{color:#16a34a}
h1.err{color:#dc2626}
h1.info{color:#2563eb}
p{margin:6px 0;line-height:1.5}
.hint{color:#9ca3af;font-size:13px;margin-top:18px}
</style></head><body>
<div class="box">
<h1 class="{{.CSSClass}}">{{.Heading}}</h1>
<p>{{.Message}}</p>
{{if .AutoClose}}<p class="hint">2.5 秒后自动关闭…</p>
<script>setTimeout(function(){try{window.close();}catch(e){}},2500);</script>
{{else}}<p class="hint">可关闭此页面</p>{{end}}
</div></body></html>`))

func renderCaptureHTML(c *gin.Context, r captureResult) {
	heading := "✅ 抓取成功"
	cssClass := "ok"
	autoClose := true
	switch r.Phase {
	case "error":
		heading = "❌ 抓取失败"
		cssClass = "err"
		autoClose = false
	case "unchanged":
		heading = "ℹ️ 未更新"
		cssClass = "info"
	case "updated":
		heading = "✅ 已更新"
	case "created":
		heading = "✅ 已收藏"
	}
	c.Status(r.Status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = captureResultPage.Execute(c.Writer, struct {
		Heading   string
		CSSClass  string
		Message   string
		AutoClose bool
	}{heading, cssClass, r.Message, autoClose})
}

func (h *BookmarkletHandler) authenticate(token string) (*model.User, error) {
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
// pulls out the largest body of text it can find using the same selector
// strategy as internal/rss/content.go::fetchDirect.
func extractContentFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Remove()

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
		c := selectionText(nodes.First())
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
			t := strings.TrimSpace(s.Text())
			if len(t) > 30 {
				b.WriteString(t)
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

// selectionText pulls block-level text out of a goquery Selection in a way
// that preserves paragraph breaks. Mirrors rss.extractText's behavior.
func selectionText(s *goquery.Selection) string {
	var b strings.Builder
	s.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre").Each(func(i int, sel *goquery.Selection) {
		t := strings.TrimSpace(sel.Text())
		if len(t) > 20 {
			b.WriteString(t)
			b.WriteString("\n\n")
		}
	})
	if b.Len() > 200 {
		return b.String()
	}
	return strings.TrimSpace(s.Text())
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
