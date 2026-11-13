package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/httpx"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/pdfextract"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

const (
	maxLinkSetParentsPerCycle   = 10
	maxLinkSetChildrenPerCycle  = 30
	maxSuggestionChecksPerCycle = 10
	linkSetMinCandidates        = 3
)

// Keep network access injectable while both production fetch methods retain
// their existing behavior.
type linkSetContentFetcher interface {
	FetchHTMLDocument(context.Context, string) (*goquery.Document, error)
	FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error)
}

// detectLinkSetCandidates inspects articles whose links_extendable is NULL
// (not yet checked), fetches their raw HTML, runs ExtractCandidates, and
// flips the flag: true if count >= linkSetMinCandidates, false otherwise.
// It does NOT create any child articles — that's deferred to the user's
// explicit batch_fetch action.
func detectLinkSetCandidates(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	contentFetcher linkSetContentFetcher,
) {
	arts, err := articleRepo.FindArticlesNeedingLinkCheck(maxLinkSetParentsPerCycle)
	if err != nil {
		log.Printf("link_set: find articles needing check: %v", err)
		return
	}
	if len(arts) == 0 {
		return
	}
	log.Printf("link_set: checking %d articles for extractable links", len(arts))

	for _, a := range arts {
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ownerCtx, err := articleRepo.TaskContext(fetchCtx, a.ID)
		if err != nil {
			cancel()
			log.Printf("link_set: resolve owner %d: %v", a.ID, err)
			continue
		}
		release, err := admitBackground(ownerCtx, "background_fetch")
		if err != nil {
			cancel()
			// Admission is retryable; preserve unchecked flags.
			continue
		}
		doc, err := contentFetcher.FetchHTMLDocument(ownerCtx, a.URL)
		cancel()
		release()
		if err != nil || doc == nil {
			log.Printf("link_set: %d fetch html: %v", a.ID, err)
			// Mark as checked-no so we don't keep retrying.
			if e := articleRepo.SetLinksExtendable(a.ID, false); e != nil {
				log.Printf("link_set: mark false on fetch fail %d: %v", a.ID, e)
			}
			continue
		}
		rawHTML, _ := doc.Html()
		cands := rss.ExtractCandidates(rawHTML, a.URL)
		extendable := len(cands) >= linkSetMinCandidates

		if extendable {
			repoCands := make([]repository.LinkSetCandidate, 0, len(cands))
			for i, c := range cands {
				repoCands = append(repoCands, repository.LinkSetCandidate{
					ParentArticleID: a.ID,
					Title:           c.Title,
					URL:             c.URL,
					EditorNote:      c.EditorNote,
					Position:        i,
				})
			}
			if err := articleRepo.ReplaceLinkSetCandidates(a.ID, repoCands); err != nil {
				log.Printf("link_set: cache candidates for %d: %v", a.ID, err)
				// continue anyway — the flag still flips so the user gets the button,
				// and the endpoint will fall back to live extraction if needed.
			}
		}

		if err := articleRepo.SetLinksExtendable(a.ID, extendable); err != nil {
			log.Printf("link_set: set extendable %d: %v", a.ID, err)
			continue
		}
		log.Printf("link_set: %d → %d candidates → extendable=%v", a.ID, len(cands), extendable)
	}
}

// detectLinkSetSuggestions inspects rss-typed articles that haven't been
// checked by the suggestion detector yet, fetches their HTML, and writes
// link_set_suggested = true when the article looks like a link list (>=11
// candidate links forming a one-per-line sibling run with <=2 gap segments).
// false otherwise so we don't keep re-checking. Caches the qualifying
// candidates so the batch-fetch modal opens instantly after the user
// confirms.
func detectLinkSetSuggestions(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	contentFetcher linkSetContentFetcher,
) {
	arts, err := articleRepo.FindArticlesNeedingSuggestionCheck(maxSuggestionChecksPerCycle)
	if err != nil {
		log.Printf("link_set suggest: find articles needing check: %v", err)
		return
	}
	if len(arts) == 0 {
		return
	}
	log.Printf("link_set suggest: checking %d rss articles", len(arts))

	for _, a := range arts {
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ownerCtx, err := articleRepo.TaskContext(fetchCtx, a.ID)
		if err != nil {
			cancel()
			log.Printf("link_set: resolve owner %d: %v", a.ID, err)
			continue
		}
		release, err := admitBackground(ownerCtx, "background_fetch")
		if err != nil {
			cancel()
			// Admission is retryable; preserve unchecked flags.
			continue
		}
		doc, err := contentFetcher.FetchHTMLDocument(ownerCtx, a.URL)
		cancel()
		release()
		if err != nil || doc == nil {
			log.Printf("link_set suggest: %d fetch html: %v", a.ID, err)
			if e := articleRepo.SetLinkSetSuggested(a.ID, false); e != nil {
				log.Printf("link_set suggest: mark false on fetch fail %d: %v", a.ID, e)
			}
			continue
		}
		rawHTML, _ := doc.Html()
		cands, qualifies := rss.DetectLinkSetSuggestion(rawHTML, a.URL)

		if qualifies {
			repoCands := make([]repository.LinkSetCandidate, 0, len(cands))
			for i, c := range cands {
				repoCands = append(repoCands, repository.LinkSetCandidate{
					ParentArticleID: a.ID,
					Title:           c.Title,
					URL:             c.URL,
					EditorNote:      c.EditorNote,
					Position:        i,
				})
			}
			if err := articleRepo.ReplaceLinkSetCandidates(a.ID, repoCands); err != nil {
				log.Printf("link_set suggest: cache candidates for %d: %v", a.ID, err)
			}
		}

		if err := articleRepo.SetLinkSetSuggested(a.ID, qualifies); err != nil {
			log.Printf("link_set suggest: set suggested %d: %v", a.ID, err)
			continue
		}
		log.Printf("link_set suggest: %d → %d candidates → suggested=%v", a.ID, len(cands), qualifies)
	}
}

func runLinkFetchLoop(ctx context.Context, interval time.Duration, process func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		process(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processQueuedChildren fetches content for the queued link-set children.
//
// imageBaseDir is the same cfg.Backup.Dir used by the PDF capture flow —
// when a child URL is a PDF we route it through pdfextract instead of
// the HTML scraper, writing image rasters to the same article_images
// path layout so the image-serve endpoint finds them.
func processQueuedChildren(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	contentFetcher linkSetContentFetcher,
	imageBaseDir string,
) {
	children, err := articleRepo.GetProcessingChildren(maxLinkSetChildrenPerCycle)
	if err != nil {
		log.Printf("link_set: get processing children: %v", err)
		return
	}
	if len(children) == 0 {
		return
	}
	log.Printf("link_set: fetching content for %d queued children", len(children))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentContent)
	var success, failed int64

	for i := range children {
		c := &children[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			cctx, ownerErr := articleRepo.TaskContext(cctx, c.ID)
			if ownerErr != nil {
				log.Printf("link_set: resolve child owner %d: %v", c.ID, ownerErr)
				return
			}
			release, budgetErr := admitBackground(cctx, "background_fetch")
			if budgetErr != nil {
				// Keep processing state and retry attempts untouched until admission.
				return
			}
			defer release()

			pending, err := articleRepo.LinkFetchPending(cctx, c.ID)
			if err != nil || !pending {
				return
			}
			go watchLinkFetch(cctx, articleRepo, c.ID, cancel)

			if err := fetchLinkChild(cctx, articleRepo, contentFetcher, c, imageBaseDir); err != nil {
				if cctx.Err() != nil && ctx.Err() != nil {
					return
				}
				log.Printf("link_set: child %d fetch failed: %v", c.ID, err)
				if e := articleRepo.IncrementRefetchAttempts(c.ID); e != nil {
					log.Printf("link_set: increment refetch_attempts %d: %v", c.ID, e)
				}
				if e := articleRepo.MarkFailedAfterRetries(c.ID, 3); e != nil {
					log.Printf("link_set: mark failed %d: %v", c.ID, e)
				}
				atomic.AddInt64(&failed, 1)
				return
			}
			atomic.AddInt64(&success, 1)
		}()
	}
	wg.Wait()
	log.Printf("link_set: queued children pass done — %d ok, %d failed", success, failed)
}

// A deleted task may already be in an HTTP request. Stop that request when
// the deletion becomes visible. The conditional write also protects the
// interval between checks.
func watchLinkFetch(ctx context.Context, repo *repository.ArticleRepository, id int, cancel context.CancelFunc) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pending, err := repo.LinkFetchPending(ctx, id)
			if err == nil && !pending {
				cancel()
				return
			}
		}
	}
}

func fetchLinkChild(ctx context.Context, repo *repository.ArticleRepository, fetcher linkSetContentFetcher, child *model.Article, imageBaseDir string) error {
	if looksLikePDFURL(child.URL) {
		return processPDFChild(ctx, repo, child, imageBaseDir)
	}
	result, err := fetcher.FetchContentWithMetadata(ctx, child.URL)
	if err != nil {
		return err
	}
	// Postgres TEXT cannot store NUL bytes.
	content := strings.ReplaceAll(result.Content, "\x00", "")
	if content == "" {
		return errors.New("empty content")
	}
	words, minutes := rss.ComputeMetrics(content)
	_, err = repo.CompleteLinkFetch(child.ID, result.Title, content, words, minutes)
	return err
}

// looksLikePDFURL returns true when the URL path ends in .pdf (case-
// insensitive, ignoring query/fragment). Conservative — we'd rather
// miss a Content-Type-only PDF and fall through to FetchContent than
// route an HTML article through pdfextract.
func looksLikePDFURL(u string) bool {
	if u == "" {
		return false
	}
	p := strings.ToLower(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
	return strings.HasSuffix(p, ".pdf")
}

// processPDFChild handles a link_set child whose URL is a PDF.
// Mirrors the sync fast-path of processPDFCapture: fetch, ExtractFast,
// write images, BuildMarkdown, UpdateContent. Returns an error on any
// step; caller increments refetch + marks failed after retries.
//
// Scanned PDFs (ErrNoText) intentionally fail here rather than being
// queued for OCR — the link_set worker isn't the right place to manage
// the OCR pipeline. The user can re-clip the URL via the PDF entry to
// get it into GetPDFOCRPending.
func processPDFChild(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	c *model.Article,
	imageBaseDir string,
) error {
	pdfBytes, err := downloadPDF(ctx, httpx.NewClient(pdfOCRFetchTimeout), c.URL)
	if err != nil {
		return fmt.Errorf("download pdf: %w", err)
	}
	r, err := pdfextract.ExtractFast(ctx, pdfBytes)
	if err != nil {
		if errors.Is(err, pdfextract.ErrNoText) {
			return errors.New("PDF appears to be a scan with no text layer; re-clip the URL via the PDF entry to run OCR")
		}
		return fmt.Errorf("extract: %w", err)
	}
	if err := pdfextract.WriteImages(imageBaseDir, c.ID, collectImagesFromPages(r.Pages)); err != nil {
		log.Printf("link_set: pdf child %d write images: %v", c.ID, err)
	}
	pdfextract.BuildMarkdown(&r, c.ID)
	content := r.Markdown
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	wc, rm := rss.ComputeMetrics(content)
	saved, err := articleRepo.CompleteLinkFetch(c.ID, r.Title, content, wc, rm)
	if !saved {
		_ = pdfextract.RemoveImageDir(imageBaseDir, c.ID)
	}
	return err
}
