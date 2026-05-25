package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/pdfextract"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

// pdfOCRMu serializes processPDFOCR invocations. The ticker fires every
// 60s but a single multi-page scan can easily exceed that, so without
// this guard two goroutines would call GetPDFOCRPending in parallel and
// race on the same rows. TryLock + immediate skip is the pattern used by
// runFetchCycle in main.go.
var pdfOCRMu sync.Mutex

const (
	// maxPDFOCRPerCycle caps how many pending PDFs we touch per worker
	// tick. OCR is CPU-bound and Tesseract on a 10-page scan can take
	// 30 s+ per article, so a small cap keeps the worker responsive to
	// other loops (feed fetch, summary backfill, etc.).
	maxPDFOCRPerCycle = 5

	// pdfOCRFetchTimeout caps the upstream HTTP GET that re-downloads
	// the PDF bytes from article.URL. Mirrors pdfFetchTimeout in the
	// HTTP handler.
	pdfOCRFetchTimeout = 30 * time.Second

	// pdfOCRItemTimeout bounds total wall-clock time spent on one
	// pending PDF (fetch + ExtractWithOCR + WriteImages + DB update).
	// Tesseract on a pathological scan can hang forever otherwise.
	pdfOCRItemTimeout = 5 * time.Minute

	// pdfOCRMaxBytes mirrors the HTTP layer's 32 MiB cap so a malicious
	// URL change between capture and OCR can't blow up worker memory.
	pdfOCRMaxBytes = 32 << 20
)

// processPDFOCR is the per-cycle entry point for the async OCR queue.
// Walks GetPDFOCRPending serially (concurrency = 1: OCR is CPU-bound,
// goroutines would just thrash the cores) and runs runOnePDFOCR on
// each. Best-effort: per-item failures are logged and the article is
// marked failed; the loop continues.
func processPDFOCR(ctx context.Context, articleRepo *repository.ArticleRepository, cfg config.Config) {
	if !pdfOCRMu.TryLock() {
		log.Println("pdf_ocr: previous cycle still in flight, skipping this tick")
		return
	}
	defer pdfOCRMu.Unlock()

	pending, err := articleRepo.GetPDFOCRPending(maxPDFOCRPerCycle)
	if err != nil {
		log.Printf("pdf_ocr: get pending: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	log.Printf("pdf_ocr: processing %d pending PDFs", len(pending))

	for i := range pending {
		// Respect cancellation between items so a shutdown doesn't have
		// to wait for the whole queue.
		if ctx.Err() != nil {
			log.Printf("pdf_ocr: context cancelled, stopping cycle")
			return
		}
		runOnePDFOCR(ctx, articleRepo, cfg, &pending[i])
	}
}

// runOnePDFOCR processes a single queued PDF article: re-fetches bytes
// from a.URL, runs OCR, writes images, builds markdown, updates the row.
// Each call gets its own pdfOCRItemTimeout context so a stuck Tesseract
// can't block the whole queue.
//
// Eager-images race note: Phase 3A pre-writes images via ExtractFast
// when stubbing the article. ExtractWithOCR returns its own image set
// with possibly different idx values; WriteImages overwrites at the
// idx-keyed paths. The markdown we build below references the OCR pass's
// idx values, so the rendered article stays internally consistent —
// any leftover Phase-3A files with idx values not produced by this pass
// are orphaned but harmless (the markdown never references them).
func runOnePDFOCR(parentCtx context.Context, articleRepo *repository.ArticleRepository, cfg config.Config, a *model.Article) {
	itemCtx, cancel := context.WithTimeout(parentCtx, pdfOCRItemTimeout)
	defer cancel()

	// 1. file:// and other non-http(s) schemes — worker can't read the
	//    user's local disk. Fail fast with a clear message; the
	//    bookmarklet currently won't queue these (it sends the bytes
	//    via the multipart endpoint), but a future capture path or a
	//    manually inserted row could.
	if !isHTTPURL(a.URL) {
		msg := "本地 PDF 异步 OCR 不可用：worker 无法读取 file:// 路径，请重新通过浏览器扩展上传 PDF 二进制"
		if err := articleRepo.MarkPDFFailed(a.ID, msg); err != nil {
			log.Printf("pdf_ocr: mark failed (non-http) id=%d: %v", a.ID, err)
		}
		log.Printf("pdf_ocr: article %d has non-http URL %q, marked failed", a.ID, a.URL)
		return
	}

	// 2. Re-fetch the PDF bytes. Use a sub-context so the HTTP timeout
	//    is independent of (but bounded by) the item-level timeout.
	pdfBytes, err := fetchPDFBytes(itemCtx, a.URL)
	if err != nil {
		log.Printf("pdf_ocr: refetch id=%d url=%s: %v", a.ID, a.URL, err)
		if e := articleRepo.MarkPDFFailed(a.ID, "重新下载 PDF 失败，请稍后重试或重新通过浏览器扩展上传"); e != nil {
			log.Printf("pdf_ocr: mark failed (refetch) id=%d: %v", a.ID, e)
		}
		return
	}

	// 3. Run OCR. ExtractWithOCR records per-page OCR failures inline in
	//    the page text rather than failing the whole call; a non-nil
	//    error here means the pipeline itself (pdftoppm, etc.) blew up.
	r, err := pdfextract.ExtractWithOCR(itemCtx, pdfBytes)
	if err != nil {
		log.Printf("pdf_ocr: OCR pipeline id=%d url=%s: %v", a.ID, a.URL, err)
		if e := articleRepo.MarkPDFFailed(a.ID, "OCR 处理失败，请重试或检查 PDF 是否可读"); e != nil {
			log.Printf("pdf_ocr: mark failed (ocr) id=%d: %v", a.ID, e)
		}
		return
	}

	// 4. Persist images (best-effort — text-only is still useful) and
	//    build markdown.
	imgs := collectImagesFromPages(r.Pages)
	if err := pdfextract.WriteImages(cfg.Backup.Dir, a.ID, imgs); err != nil {
		log.Printf("pdf_ocr: WriteImages id=%d: %v", a.ID, err)
	}
	pdfextract.BuildMarkdown(&r, a.ID)
	content := r.Markdown
	if len(content) > 50000 {
		// Match the cap used by the HTML scrape path and the sync PDF
		// path so the column stays in a predictable size band.
		content = content[:50000] + "..."
	}

	// 5. Update content (transitions processing_state to 'ready' and
	//    clears any prior processing_error).
	wc, rm := rss.ComputeMetrics(content)
	if err := articleRepo.UpdateContentAndMarkReady(a.ID, content, wc, rm); err != nil {
		log.Printf("pdf_ocr: update content id=%d: %v", a.ID, err)
		// Don't MarkPDFFailed here — the DB write failed, so a follow-up
		// MarkPDFFailed would likely also fail. The article stays in
		// 'processing' and we retry next cycle.
		return
	}

	// 6. Optionally refresh the title if OCR (via the PDF /Title
	//    metadata) recovered a better one than the stub had.
	newTitle := strings.TrimSpace(r.Title)
	if newTitle != "" && newTitle != a.Title {
		if err := articleRepo.UpdateTitle(a.ID, newTitle); err != nil {
			log.Printf("pdf_ocr: update title id=%d: %v", a.ID, err)
		}
	}

	log.Printf("pdf_ocr: completed id=%d (%d chars, %d images)", a.ID, len(content), len(imgs))
}

// fetchPDFBytes re-downloads the PDF from rawURL. Caller is responsible
// for the surrounding timeout/cancellation context.
func fetchPDFBytes(ctx context.Context, rawURL string) ([]byte, error) {
	client := &http.Client{
		Timeout: pdfOCRFetchTimeout,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RSS-Pal-PDF-Worker/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	// LimitReader at cap+1 so we can distinguish "exactly at cap" from
	// "over cap".
	body, err := io.ReadAll(io.LimitReader(resp.Body, pdfOCRMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > pdfOCRMaxBytes {
		return nil, fmt.Errorf("PDF exceeds %d byte cap", pdfOCRMaxBytes)
	}
	return body, nil
}

// isHTTPURL returns true iff rawURL has scheme http or https. Used to
// reject file://, gopher://, data:, etc. before we try to refetch.
func isHTTPURL(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// collectImagesFromPages flattens per-page image refs for WriteImages.
// Mirrors collectImages in bookmarklet_pdf.go; duplicated here so the
// worker package doesn't depend on the api package.
func collectImagesFromPages(pages []pdfextract.PageContent) []pdfextract.ImageRef {
	var out []pdfextract.ImageRef
	for _, p := range pages {
		out = append(out, p.Images...)
	}
	return out
}
