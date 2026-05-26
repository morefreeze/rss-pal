package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

const (
	maxLinkSetParentsPerCycle     = 10
	maxLinkSetChildrenPerCycle    = 30
	maxSuggestionChecksPerCycle   = 10
	linkSetMinCandidates          = 3
)

// detectLinkSetCandidates inspects articles whose links_extendable is NULL
// (not yet checked), fetches their raw HTML, runs ExtractCandidates, and
// flips the flag: true if count >= linkSetMinCandidates, false otherwise.
// It does NOT create any child articles — that's deferred to the user's
// explicit batch_fetch action.
func detectLinkSetCandidates(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	contentFetcher *rss.ContentFetcher,
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
		doc, err := contentFetcher.FetchHTMLDocument(fetchCtx, a.URL)
		cancel()
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
	contentFetcher *rss.ContentFetcher,
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
		doc, err := contentFetcher.FetchHTMLDocument(fetchCtx, a.URL)
		cancel()
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

// processQueuedChildren handles processing_state='processing' children:
// fetches content. The existing backfillSummaries pass then picks them up
// and transitions to 'ready' via UpdateSummary.
func processQueuedChildren(
	ctx context.Context,
	articleRepo *repository.ArticleRepository,
	contentFetcher *rss.ContentFetcher,
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
			sem <- struct{}{}
			defer func() { <-sem }()

			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			content, ferr := contentFetcher.FetchContent(cctx, c.URL)
			if ferr != nil || content == "" {
				log.Printf("link_set: child %d (%s) fetch failed: %v", c.ID, c.URL, ferr)
				if e := articleRepo.IncrementRefetchAttempts(c.ID); e != nil {
					log.Printf("link_set: increment refetch_attempts %d: %v", c.ID, e)
				}
				if e := articleRepo.MarkFailedAfterRetries(c.ID, 3); e != nil {
					log.Printf("link_set: mark failed %d: %v", c.ID, e)
				}
				atomic.AddInt64(&failed, 1)
				return
			}
			// PDFs and other binaries can ship as the "content" of a URL
			// (link_set's ContentFetcher doesn't enforce text/html). NUL
			// bytes inside violate Postgres TEXT encoding and make
			// UpdateContent fail forever. Strip NULs so the write
			// succeeds; if the result is still mostly binary that's a
			// pre-existing scrape-quality issue, not a hard failure.
			if strings.ContainsRune(content, '\x00') {
				content = strings.ReplaceAll(content, "\x00", "")
			}
			wc, rm := rss.ComputeMetrics(content)
			if e := articleRepo.UpdateContent(c.ID, content, wc, rm); e != nil {
				log.Printf("link_set: save content %d: %v", c.ID, e)
				// Treat persist failure the same as fetch failure so the
				// worker doesn't loop forever on a stuck row. Previously
				// this branch only logged and returned, keeping the row
				// in processing_state='processing' indefinitely.
				if ie := articleRepo.IncrementRefetchAttempts(c.ID); ie != nil {
					log.Printf("link_set: increment refetch_attempts %d: %v", c.ID, ie)
				}
				if me := articleRepo.MarkFailedAfterRetries(c.ID, 3); me != nil {
					log.Printf("link_set: mark failed %d: %v", c.ID, me)
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
