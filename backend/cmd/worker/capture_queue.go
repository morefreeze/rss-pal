package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

type captureContentFetcher interface {
	FetchContentWithMetadata(context.Context, string) (rss.ContentResult, error)
}

type captureQueueRunner struct {
	mu      sync.Mutex
	afterID int
}

// One worker drains a bounded batch independently of feed discovery and OCR.
// Queued rows remain durable until content is saved or three attempts fail.
func runCaptureQueue(ctx context.Context, repo *repository.ArticleRepository, fetcher captureContentFetcher) {
	runner := &captureQueueRunner{}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		runner.processQueuedCaptures(ctx, repo, fetcher)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runner *captureQueueRunner) processQueuedCaptures(ctx context.Context, repo *repository.ArticleRepository, fetcher captureContentFetcher) {
	if ctx.Err() != nil || !runner.mu.TryLock() {
		return
	}
	defer runner.mu.Unlock()
	articles, err := repo.GetQueuedCapturesAfter(runner.afterID, 10)
	if err != nil {
		log.Printf("capture_queue: list failed: %v", err)
		return
	}
	if len(articles) == 0 && runner.afterID != 0 {
		runner.afterID = 0
		articles, err = repo.GetQueuedCapturesAfter(0, 10)
		if err != nil {
			log.Printf("capture_queue: wrap failed: %v", err)
			return
		}
	}
	for _, article := range articles {
		runner.afterID = article.ID
		if ctx.Err() != nil {
			return
		}
		ownerCtx, err := repo.TaskContext(ctx, article.ID)
		if err != nil {
			log.Printf("capture_queue: owner article=%d: %v", article.ID, err)
			continue
		}
		workCtx, cancel := context.WithTimeout(ownerCtx, 60*time.Second)
		release, err := admitBackground(workCtx, "background_fetch")
		if err != nil {
			cancel()
			continue
		}
		result, fetchErr := fetcher.FetchContentWithMetadata(workCtx, article.URL)
		release()
		cancel()
		// Shutdown is not a failed publisher attempt; the next worker resumes.
		if ctx.Err() != nil {
			return
		}
		if fetchErr == nil && strings.TrimSpace(result.Content) != "" {
			content := strings.ReplaceAll(result.Content, "\x00", "")
			words, minutes := rss.ComputeMetrics(content)
			if err := repo.CompleteQueuedCapture(article.ID, content, result.Title, result.PublishedAt, words, minutes); err == nil {
				log.Printf("capture_queue: completed article=%d", article.ID)
				continue
			} else {
				log.Printf("capture_queue: save article=%d: %v", article.ID, err)
			}
		}
		if err := repo.FailQueuedCaptureAttempt(article.ID); err != nil {
			log.Printf("capture_queue: record failure article=%d: %v", article.ID, err)
		}
		log.Printf("capture_queue: retry recorded article=%d", article.ID)
	}
}
