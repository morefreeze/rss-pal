package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/pdfextract"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/gin-gonic/gin"
)

// captureMaxPDFBytes caps PDF uploads at 64 MiB. Larger than the 4 MiB
// HTML cap because PDFs are denser, especially scanned ones / image-heavy
// zines (e.g. PagedOut), while still small enough to keep one request
// from blocking the worker for too long. Previously 32 MiB but real-
// world long-form PDFs routinely brushed that ceiling.
const captureMaxPDFBytes = 64 << 20

// pdfFetchTimeout caps the upstream fetch in capture-pdf-url. 30 s is
// long enough for a slow CDN serving a 30 MB PDF on a flaky line but
// short enough that the user gets a clear failure rather than a hung
// request.
const pdfFetchTimeout = 30 * time.Second

// pdfCaptureResult is the JSON envelope both PDF capture endpoints return.
// Status discriminates the three outcomes: "created" (new article, fully
// extracted), "updated" (existing article re-extracted), and "processing"
// (queued for async OCR — the worker will fill in the content later).
type pdfCaptureResult struct {
	Status    string `json:"status"` // "created" | "updated" | "processing"
	ArticleID int    `json:"article_id"`
	Message   string `json:"message"`
}

// processPDFCapture is the shared core for both capture-pdf and
// capture-pdf-url. It runs the sync fast-path extraction; on ErrNoText
// (likely scanned PDF) it stashes a stub article in 'processing' state
// for the worker to OCR later. Otherwise it persists the full content
// synchronously.
//
// ctx flows into pdfextract.ExtractFast so a cancelled request short-
// circuits the (potentially-slow) pdftotext/pdfimages shell-outs.
func (h *BookmarkletHandler) processPDFCapture(
	c *gin.Context,
	user *model.User,
	rawURL, browserTitle string,
	pdfBytes []byte,
	imageBaseDir string,
) (pdfCaptureResult, error) {
	ctx := c.Request.Context()
	normalized := util.NormalizeURLKeepFragment(rawURL)

	// 1. Sync-attempt extraction. ErrNoText is the documented signal
	//    that text was too sparse to be useful — fall through to async OCR.
	r, fastErr := pdfextract.ExtractFast(ctx, pdfBytes)
	queueForOCR := false
	if fastErr != nil {
		if !errors.Is(fastErr, pdfextract.ErrNoText) {
			return pdfCaptureResult{}, fmt.Errorf("extract: %w", fastErr)
		}
		queueForOCR = true
	}

	// 2. Title priority: PDF /Title metadata → browser document.title →
	//    URL filename basename → the full normalized URL.
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.TrimSpace(browserTitle)
	}
	if title == "" {
		title = filenameFromURL(rawURL)
	}
	if title == "" {
		title = normalized
	}

	// 3. Lookup/get the user's clip feed (creates on first capture).
	feed, err := h.feedRepo.WithCtx(c).GetOrCreateClipFeed(user.ID)
	if err != nil {
		return pdfCaptureResult{}, fmt.Errorf("clip feed: %w", err)
	}

	// 4. Dedup against existing article (same user, same normalized URL).
	articleRepo := h.articleRepo.WithCtx(c)
	existing, err := articleRepo.FindByOwnerAndURL(user.ID, normalized)
	if err != nil {
		return pdfCaptureResult{}, fmt.Errorf("lookup: %w", err)
	}

	// 5a. Async (OCR) branch. The stub carries processing_state='processing'
	//     and empty content; the worker picks it up via GetPDFOCRPending
	//     and runs ExtractWithOCR. Images are still extracted now (cheap
	//     pdfimages pass) so figures render before OCR text fills in.
	if queueForOCR {
		if existing != nil {
			// Bookmarklet re-capture of an already-queued (or previously
			// failed) PDF — flip the article back to 'processing' so the
			// worker's GetPDFOCRPending picks it up on the next tick.
			// (MarkPDFFailed leaves it in 'failed' which the worker
			// explicitly skips — that was the previous bug here.) Surfaces
			// "processing" to the user so they know we accepted the retry.
			if err := articleRepo.ResetPDFToProcessing(existing.ID); err != nil {
				log.Printf("pdf: reset existing %d to processing (re-queue): %v", existing.ID, err)
			}
			return pdfCaptureResult{
				Status:    "processing",
				ArticleID: existing.ID,
				Message:   "已重新加入 OCR 队列",
			}, nil
		}
		article := &model.Article{
			FeedID: feed.ID,
			Title:  title,
			URL:    normalized,
			IsClip: true,
		}
		if err := articleRepo.CreatePDFStub(article); err != nil {
			return pdfCaptureResult{}, fmt.Errorf("create stub: %w", err)
		}
		// Best-effort image dump while we have the bytes. Failure to write
		// images is non-fatal — text will catch up after OCR and figures
		// can be re-extracted then.
		if err := pdfextract.WriteImages(imageBaseDir, article.ID, collectImages(r.Pages)); err != nil {
			log.Printf("pdf: WriteImages for stub article=%d: %v", article.ID, err)
		}
		return pdfCaptureResult{
			Status:    "processing",
			ArticleID: article.ID,
			Message:   "已加入 OCR 队列：" + title,
		}, nil
	}

	// 5b. Sync (digital PDF) branch — full content available immediately.
	var article *model.Article
	if existing != nil {
		article = existing
	} else {
		article = &model.Article{
			FeedID: feed.ID,
			Title:  title,
			URL:    normalized,
			IsClip: true,
		}
		if err := articleRepo.Create(article); err != nil {
			return pdfCaptureResult{}, fmt.Errorf("create: %w", err)
		}
	}

	// Write images first so the markdown image refs resolve against real
	// files on disk. Non-fatal — text-only is still useful.
	imgs := collectImages(r.Pages)
	if err := pdfextract.WriteImages(imageBaseDir, article.ID, imgs); err != nil {
		log.Printf("pdf: WriteImages for article=%d: %v", article.ID, err)
	}

	pdfextract.BuildMarkdown(&r, article.ID)
	content := r.Markdown
	if len(content) > 50000 {
		// Match the cap used by the HTML scrape path so the column stays
		// in a predictable size band.
		content = content[:50000] + "..."
	}
	wc, rm := rss.ComputeMetrics(content)
	if err := articleRepo.UpdateContentAndMarkReady(article.ID, content, wc, rm); err != nil {
		return pdfCaptureResult{}, fmt.Errorf("update content: %w", err)
	}
	if title != "" && title != article.Title {
		if err := articleRepo.UpdateTitle(article.ID, title); err != nil {
			log.Printf("pdf: UpdateTitle for article=%d: %v", article.ID, err)
		}
	}
	// Clearing the cached summary forces the worker's backfillSummaries
	// loop to regenerate from the freshly extracted content.
	if err := articleRepo.UpdateSummary(article.ID, "", ""); err != nil {
		log.Printf("pdf: clear summary for article=%d: %v", article.ID, err)
	}

	status := "created"
	if existing != nil {
		status = "updated"
	}
	return pdfCaptureResult{
		Status:    status,
		ArticleID: article.ID,
		Message:   "已加入网摘：" + title,
	}, nil
}

// collectImages flattens per-page image refs into one slice for
// pdfextract.WriteImages. Order is page-major, preserving the per-page
// order returned by extractImages.
func collectImages(pages []pdfextract.PageContent) []pdfextract.ImageRef {
	var imgs []pdfextract.ImageRef
	for _, p := range pages {
		imgs = append(imgs, p.Images...)
	}
	return imgs
}

// CapturePDF is POST /api/bookmarklet/capture-pdf. It receives a
// multipart form with three fields:
//   - url   (required) — the page URL the PDF was viewed at (used for dedup)
//   - title (optional) — document.title from the bookmarklet
//   - file  (required) — the PDF bytes, application/pdf
//
// Status codes mirror processPDFCapture's outcomes:
//   - 201 Created   — new article extracted synchronously
//   - 200 OK        — existing article re-extracted
//   - 202 Accepted  — queued for OCR (scanned PDF)
func (h *BookmarkletHandler) CapturePDF(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 bookmarklet token"})
		return
	}

	// Hard-cap the request body before the multipart parser allocates;
	// any oversize payload short-circuits with 413.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captureMaxPDFBytes)
	if err := c.Request.ParseMultipartForm(captureMaxPDFBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "PDF 过大"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "表单解析失败"})
		return
	}

	rawURL := strings.TrimSpace(c.Request.FormValue("url"))
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 必填"})
		return
	}
	browserTitle := strings.TrimSpace(c.Request.FormValue("title"))

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file 必填"})
		return
	}
	if fileHeader.Size > captureMaxPDFBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "PDF 过大"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取上传文件"})
		return
	}
	defer f.Close()
	pdfBytes, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 PDF 失败"})
		return
	}

	res, err := h.processPDFCapture(c, user, rawURL, browserTitle, pdfBytes, h.imageBaseDir)
	if err != nil {
		log.Printf("CapturePDF user=%d url=%s: %v", user.ID, rawURL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理 PDF 失败"})
		return
	}
	if h.backup != nil {
		h.backup.TriggerAsync()
	}
	c.JSON(pdfStatusToHTTP(res.Status), res)
}

// CapturePDFURL is POST /api/bookmarklet/capture-pdf-url. Body:
// {"url": "..."}. The server fetches the PDF itself (30 s timeout,
// 32 MiB cap), so the bookmarklet doesn't have to ship binary bytes
// from the user's browser — useful when the original tab can't grab
// the PDF buffer (CORS, file://, etc.).
func (h *BookmarkletHandler) CapturePDFURL(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 bookmarklet token"})
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 必填"})
		return
	}

	// SSRF mitigation: only allow http/https. Rejects file://, gopher://,
	// data:, and other exotic schemes that net/http would otherwise honour.
	// NOTE: this does NOT block private-IP targets (10/8, 127/8, 169.254/16,
	// link-local v6, etc.). Adding a DNS-resolve + IP-range check is the
	// natural next step but requires either resolving twice (TOCTOU) or a
	// custom Dialer — left as a follow-up.
	parsedURL, err := url.Parse(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 http/https 的 PDF 链接"})
		return
	}

	client := &http.Client{
		Timeout: pdfFetchTimeout,
		// Defense in depth: re-validate each redirect target so a clever
		// upstream can't redirect us to file:// or loop indefinitely. The
		// stdlib default already caps at 10 redirects but we restate it
		// here next to the scheme check so the policy is in one place.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to disallowed scheme: %s", req.URL.Scheme)
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, req.URL, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 无效"})
		return
	}
	httpReq.Header.Set("User-Agent", "RSS-Pal-PDF-Fetch/1.0")
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("CapturePDFURL fetch failed user=%d url=%s: %v", user.ID, req.URL, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "下载 PDF 失败"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("CapturePDFURL upstream status user=%d url=%s status=%d", user.ID, req.URL, resp.StatusCode)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("下载 PDF 失败：HTTP %d", resp.StatusCode)})
		return
	}
	// Accept either an honest application/pdf Content-Type or a URL that
	// ends in .pdf — some CDNs serve octet-stream for PDF objects.
	ct := resp.Header.Get("Content-Type")
	lowerPath := strings.SplitN(strings.ToLower(req.URL), "?", 2)[0]
	if !strings.HasPrefix(ct, "application/pdf") && !strings.HasSuffix(lowerPath, ".pdf") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "目标不是 PDF：Content-Type=" + ct})
		return
	}
	// Read at most captureMaxPDFBytes+1 so we can distinguish "exactly at
	// the cap" from "over the cap" without buffering the whole rest of
	// the response.
	pdfBytes, err := io.ReadAll(io.LimitReader(resp.Body, captureMaxPDFBytes+1))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取响应失败"})
		return
	}
	if int64(len(pdfBytes)) > captureMaxPDFBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "PDF 过大"})
		return
	}

	res, err := h.processPDFCapture(c, user, req.URL, "", pdfBytes, h.imageBaseDir)
	if err != nil {
		log.Printf("CapturePDFURL user=%d url=%s: %v", user.ID, req.URL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理 PDF 失败"})
		return
	}
	if h.backup != nil {
		h.backup.TriggerAsync()
	}
	c.JSON(pdfStatusToHTTP(res.Status), res)
}

// pdfStatusToHTTP maps the pdfCaptureResult.Status discriminator to the
// HTTP status code the bookmarklet should see. Defaults to 201 (the
// most common path: new article created in the sync fast path).
func pdfStatusToHTTP(status string) int {
	switch status {
	case "updated":
		return http.StatusOK
	case "processing":
		return http.StatusAccepted
	default:
		return http.StatusCreated
	}
}

// filenameFromURL strips query/fragment and returns the basename of the
// URL path minus a trailing .pdf/.PDF extension. Returns "" when the
// path collapses to "." (e.g. empty URL).
func filenameFromURL(rawURL string) string {
	u := strings.SplitN(rawURL, "?", 2)[0]
	u = strings.SplitN(u, "#", 2)[0]
	name := filepath.Base(u)
	// filepath.Base of "" or "." both yield "." — collapse that to "" so
	// the caller can detect "no useful filename".
	if name == "." {
		return ""
	}
	name = strings.TrimSuffix(name, ".pdf")
	name = strings.TrimSuffix(name, ".PDF")
	return name
}
