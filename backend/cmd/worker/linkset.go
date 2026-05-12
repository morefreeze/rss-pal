package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/service"
)

const (
	maxLinkSetParentsPerCycle  = 10
	maxLinkSetChildrenPerCycle = 30
	linkSetTopK                = 5
	linkSetSmallIssueThreshold = 8
)

// processLinkSetParents finds parent articles (is_link_set=true) that haven't
// been expanded yet, extracts their candidate links, pre-ranks against the
// owning user's signals, and inserts child rows. Top-K go in with
// processing_state='processing' (eager); the rest as 'stub' (on-demand).
func processLinkSetParents(
	ctx context.Context,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
	prefRepo *repository.PreferenceRepository,
	contentFetcher *rss.ContentFetcher,
) {
	parents, err := articleRepo.FindParentsNeedingExpansion(maxLinkSetParentsPerCycle)
	if err != nil {
		log.Printf("link_set: find parents: %v", err)
		return
	}
	if len(parents) == 0 {
		return
	}
	log.Printf("link_set: expanding %d parents", len(parents))

	for _, parent := range parents {
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)
		doc, err := contentFetcher.FetchHTMLDocument(fetchCtx, parent.URL)
		fetchCancel()
		if err != nil || doc == nil {
			log.Printf("link_set: parent %d fetch raw html: %v", parent.ID, err)
			continue
		}
		rawHTML, err := doc.Html()
		if err != nil || rawHTML == "" {
			log.Printf("link_set: parent %d empty raw html", parent.ID)
			continue
		}
		cands := rss.ExtractCandidates(rawHTML, parent.URL)
		if len(cands) == 0 {
			log.Printf("link_set: parent %d yielded no candidates", parent.ID)
			continue
		}

		// Look up owning user for ranking signals.
		feed, ferr := feedRepo.GetByID(parent.FeedID)
		var topics []model.InterestTopic
		var hosts *model.HostSignalSet
		if ferr != nil {
			log.Printf("link_set: feed %d for parent %d: %v", parent.FeedID, parent.ID, ferr)
		} else if feed.OwnerID != nil {
			if t, terr := prefRepo.GetTopByUser(*feed.OwnerID, 10); terr == nil {
				topics = t
			}
			if h, herr := prefRepo.GetUserSignalHosts(*feed.OwnerID); herr == nil {
				hosts = h
			}
		}
		if hosts == nil {
			hosts = &model.HostSignalSet{}
		}
		scores := service.PrerankCandidates(cands, topics, hosts)

		// Sort indices by score descending, document order tie-break.
		order := make([]int, len(cands))
		for i := range order {
			order[i] = i
		}
		for i := 1; i < len(order); i++ {
			for j := i; j > 0 && scores[order[j]] > scores[order[j-1]]; j-- {
				order[j], order[j-1] = order[j-1], order[j]
			}
		}

		topKLimit := linkSetTopK
		if len(cands) <= linkSetSmallIssueThreshold {
			topKLimit = len(cands)
		}
		topKSet := make(map[int]struct{})
		for i := 0; i < topKLimit && i < len(order); i++ {
			topKSet[order[i]] = struct{}{}
		}

		children := make([]repository.LinkSetChildInput, 0, len(cands))
		for i, c := range cands {
			state := "stub"
			if _, ok := topKSet[i]; ok {
				state = "processing"
			}
			children = append(children, repository.LinkSetChildInput{
				FeedID:          parent.FeedID,
				ParentArticleID: parent.ID,
				Title:           c.Title,
				URL:             c.URL,
				EditorNote:      c.EditorNote,
				PrerankScore:    scores[i],
				ProcessingState: state,
				PublishedAt:     parent.PublishedAt,
			})
		}
		n, err := articleRepo.InsertLinkSetChildren(children)
		if err != nil {
			log.Printf("link_set: insert children for parent %d: %v", parent.ID, err)
			continue
		}
		log.Printf("link_set: parent %d → %d children inserted (Top-K = %d, total candidates = %d)", parent.ID, n, topKLimit, len(cands))
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
			wc, rm := rss.ComputeMetrics(content)
			if e := articleRepo.UpdateContent(c.ID, content, wc, rm); e != nil {
				log.Printf("link_set: save content %d: %v", c.ID, e)
				return
			}
			atomic.AddInt64(&success, 1)
		}()
	}
	wg.Wait()
	log.Printf("link_set: queued children pass done — %d ok, %d failed", success, failed)
}
