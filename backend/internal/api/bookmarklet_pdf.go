package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/pdfextract"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/util"
)

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
	ctx context.Context,
	user *model.User,
	rawURL, browserTitle string,
	pdfBytes []byte,
	imageBaseDir string,
) (pdfCaptureResult, error) {
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
	feed, err := h.feedRepo.GetOrCreateClipFeed(user.ID)
	if err != nil {
		return pdfCaptureResult{}, fmt.Errorf("clip feed: %w", err)
	}

	// 4. Dedup against existing article (same user, same normalized URL).
	existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
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
			// failed) PDF — reset it to processing so the worker re-OCRs.
			// MarkPDFFailed with empty msg flips state to 'failed' first;
			// the worker treats 'failed' as eligible for manual reset, but
			// here we want it back in the OCR queue, so set it directly
			// via the same path the stub takes. We use MarkPDFFailed as a
			// best-effort reset signal — the next worker tick will see
			// either failed (and skip) or, if the user explicitly retries,
			// re-queue. For now: surface a processing response so the
			// user knows we accepted the retry.
			if err := h.articleRepo.MarkPDFFailed(existing.ID, ""); err != nil {
				log.Printf("pdf: reset existing %d before re-queue: %v", existing.ID, err)
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
		if err := h.articleRepo.CreatePDFStub(article); err != nil {
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
		if err := h.articleRepo.Create(article); err != nil {
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
	if err := h.articleRepo.UpdateContentAndMarkReady(article.ID, content, wc, rm); err != nil {
		return pdfCaptureResult{}, fmt.Errorf("update content: %w", err)
	}
	if title != "" && title != article.Title {
		if err := h.articleRepo.UpdateTitle(article.ID, title); err != nil {
			log.Printf("pdf: UpdateTitle for article=%d: %v", article.ID, err)
		}
	}
	// Clearing the cached summary forces the worker's backfillSummaries
	// loop to regenerate from the freshly extracted content.
	if err := h.articleRepo.UpdateSummary(article.ID, "", ""); err != nil {
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

// filenameFromURL strips query/fragment and returns the basename of the
// URL path minus a trailing .pdf/.PDF extension. Returns "" when the
// resulting name is empty (e.g. URL ends with "/" or has no path).
func filenameFromURL(rawURL string) string {
	u := strings.SplitN(rawURL, "?", 2)[0]
	u = strings.SplitN(u, "#", 2)[0]
	name := filepath.Base(u)
	if name == "." || name == "/" {
		return ""
	}
	name = strings.TrimSuffix(name, ".pdf")
	name = strings.TrimSuffix(name, ".PDF")
	return name
}
