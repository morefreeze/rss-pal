package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/mmcdole/gofeed"
)

var cycleMu sync.Mutex

const (
	maxConcurrentFeeds    = 5
	maxConcurrentContent  = 3
	maxConcurrentSummary  = 2
	feedTimeout           = 3 * time.Minute
	maxRefetchPerCycle    = 20
	maxNewArticlesPerFeed = 10
	maxBackfillPerCycle   = 5
)

// Global semaphore for AI summary calls to avoid hammering the API
var sumSem = make(chan struct{}, maxConcurrentSummary)

func main() {
	cfg := config.Load()

	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)

	fetcher := rss.NewFetcher()
	contentFetcher := rss.NewContentFetcher()

	var summarizer *ai.Summarizer
	if cfg.Claude.APIKey != "" {
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
		log.Println("AI summarizer initialized")
	} else {
		log.Println("CLAUDE_API_KEY not set, AI summarization disabled")
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	runFetchCycle(context.Background(), feedRepo, articleRepo, fetcher, contentFetcher, summarizer)

	for range ticker.C {
		runFetchCycle(context.Background(), feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	}
}

func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if summarizer != nil {
		backfillSummaries(ctx, articleRepo, summarizer)
	}
}

func asyncSummarize(summarizer *ai.Summarizer, articleRepo *repository.ArticleRepository, articleID int, title, content string) {
	go func() {
		sumSem <- struct{}{}
		defer func() { <-sumSem }()
		sCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result, err := summarizer.Summarize(sCtx, title, content)
		if err != nil {
			log.Printf("Failed to summarize article %d: %v", articleID, err)
			return
		}
		if err := articleRepo.UpdateSummary(articleID, result.Brief, result.Detailed); err != nil {
			log.Printf("Failed to save summary for article %d: %v", articleID, err)
		} else {
			log.Printf("Summarized article %d", articleID)
		}
	}()
}

func backfillSummaries(ctx context.Context, articleRepo *repository.ArticleRepository, summarizer *ai.Summarizer) {
	articles, err := articleRepo.GetArticlesWithoutSummary(maxBackfillPerCycle)
	if err != nil {
		log.Printf("Failed to get articles without summary: %v", err)
		return
	}

	if len(articles) == 0 {
		return
	}

	log.Printf("Backfilling summaries for %d articles", len(articles))

	var wg sync.WaitGroup
	for i := range articles {
		a := &articles[i]
		if a.Content == "" {
			continue
		}
		wg.Add(1)
		go func(article *model.Article) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			sCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			result, err := summarizer.Summarize(sCtx, article.Title, article.Content)
			if err != nil {
				log.Printf("Failed to backfill summary for article %d: %v", article.ID, err)
				return
			}
			if err := articleRepo.UpdateSummary(article.ID, result.Brief, result.Detailed); err != nil {
				log.Printf("Failed to save backfill summary for article %d: %v", article.ID, err)
			} else {
				log.Printf("Backfilled summary for article %d", article.ID)
			}
		}(a)
	}
	wg.Wait()
}

func fetchAllFeeds(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	feeds, err := feedRepo.GetAllActive()
	if err != nil {
		log.Printf("Failed to get feeds: %v", err)
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentFeeds)

	for i := range feeds {
		if !shouldFetch(&feeds[i]) {
			continue
		}

		wg.Add(1)
		go func(feed model.Feed) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			feedCtx, cancel := context.WithTimeout(ctx, feedTimeout)
			defer cancel()

			processFeed(feedCtx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer, feed)
		}(feeds[i])
	}

	wg.Wait()
}

func processFeed(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, feed model.Feed) {
	log.Printf("Fetching feed: %s", feed.URL)

	if feed.FeedType == "html" {
		processHTMLFeed(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer, feed)
		return
	}

	result, err := fetcher.Fetch(ctx, feed.URL, feed.ETag, feed.LastModified)
	if err != nil {
		log.Printf("Failed to fetch feed %s: %v", feed.URL, err)
		return
	}

	if result == nil {
		return
	}

	if err := feedRepo.UpdateFetchInfo(feed.ID, result.ETag, result.LastModified, time.Now()); err != nil {
		log.Printf("Failed to update feed info: %v", err)
	}
	if result.Feed != nil && result.Feed.Title != "" {
		if err := feedRepo.UpdateTitle(feed.ID, result.Feed.Title); err != nil {
			log.Printf("Failed to update feed title: %v", err)
		}
	}

	var (
		wg          sync.WaitGroup
		sem         = make(chan struct{}, maxConcurrentContent)
		newCount    int64
		queuedCount int
	)

	for _, item := range result.Feed.Items {
		if queuedCount >= maxNewArticlesPerFeed {
			break
		}

		exists, _ := articleRepo.Exists(feed.ID, item.Link)
		if exists {
			articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, parsePublishedTime(item.PublishedParsed, item.UpdatedParsed))
			continue
		}

		queuedCount++

		wg.Add(1)
		go func(item *gofeed.Item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content := rss.StripHTML(item.Description)
			if content == "" {
				content = rss.StripHTML(item.Content)
			}

			if item.Link != "" {
				log.Printf("Fetching full content for: %s", item.Link)
				fullContent, err := contentFetcher.FetchContent(ctx, item.Link)
				if err != nil {
					log.Printf("Failed to fetch content from %s: %v", item.Link, err)
				} else if len(fullContent) > len(content) {
					content = fullContent
					log.Printf("Got full content: %d chars", len(content))
				}
			}

			article := &model.Article{
				FeedID:      feed.ID,
				Title:       item.Title,
				URL:         item.Link,
				Content:     content,
				PublishedAt: parsePublishedTime(item.PublishedParsed, item.UpdatedParsed),
			}
			article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)

			if err := articleRepo.Create(article); err != nil {
				log.Printf("Failed to create article: %v", err)
			} else {
				atomic.AddInt64(&newCount, 1)
				if summarizer != nil {
					asyncSummarize(summarizer, articleRepo, article.ID, article.Title, article.Content)
				}
			}
		}(item)
	}

	wg.Wait()
	log.Printf("Feed %s fetched, %d new articles", feed.URL, newCount)
}

func processHTMLFeed(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, feed model.Feed) {
	htmlFeed, err := fetcher.FetchHTML(ctx, feed.URL)
	if err != nil {
		log.Printf("Failed to scrape HTML feed %s: %v", feed.URL, err)
		return
	}

	if err := feedRepo.UpdateFetchInfo(feed.ID, "", "", time.Now()); err != nil {
		log.Printf("Failed to update feed info: %v", err)
	}
	if htmlFeed.Title != "" {
		_ = feedRepo.UpdateTitle(feed.ID, htmlFeed.Title)
	}

	var (
		wg          sync.WaitGroup
		sem         = make(chan struct{}, maxConcurrentContent)
		newCount    int64
		queuedCount int
	)

	for _, item := range htmlFeed.Items {
		if queuedCount >= maxNewArticlesPerFeed || item.Link == "" {
			break
		}
		exists, _ := articleRepo.Exists(feed.ID, item.Link)
		if exists {
			articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, item.PublishedParsed)
			continue
		}
		queuedCount++
		wg.Add(1)
		go func(link, title string, pubAt *time.Time) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			content, _ := contentFetcher.FetchContent(ctx, link)
			article := &model.Article{
				FeedID:      feed.ID,
				Title:       title,
				URL:         link,
				Content:     content,
				PublishedAt: pubAt,
			}
			article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
			if err := articleRepo.Create(article); err != nil {
				log.Printf("Failed to create HTML article: %v", err)
			} else {
				atomic.AddInt64(&newCount, 1)
				if summarizer != nil {
					asyncSummarize(summarizer, articleRepo, article.ID, article.Title, article.Content)
				}
			}
		}(item.Link, item.Title, item.PublishedParsed)
	}

	wg.Wait()
	log.Printf("HTML feed %s scraped, %d new articles", feed.URL, newCount)
}

func refetchShortContent(ctx context.Context, articleRepo *repository.ArticleRepository, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	articles, err := articleRepo.GetArticlesWithShortContent(300)
	if err != nil {
		log.Printf("Failed to get articles with short content: %v", err)
		return
	}

	if len(articles) == 0 {
		return
	}

	if len(articles) > maxRefetchPerCycle {
		articles = articles[:maxRefetchPerCycle]
	}

	log.Printf("Found %d articles with short content, re-fetching...", len(articles))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentContent)

	for i := range articles {
		if articles[i].URL == "" {
			continue
		}

		wg.Add(1)
		go func(article *model.Article) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			articleCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			log.Printf("Re-fetching content for article %d: %s", article.ID, article.URL)
			content, err := contentFetcher.FetchContent(articleCtx, article.URL)
			if err != nil {
				log.Printf("Failed to re-fetch content for article %d: %v", article.ID, err)
				articleRepo.IncrementRefetchAttempts(article.ID)
				return
			}

			if len(content) > len(article.Content) {
				wc, rm := rss.ComputeMetrics(content)
				if err := articleRepo.UpdateContent(article.ID, content, wc, rm); err != nil {
					log.Printf("Failed to update content for article %d: %v", article.ID, err)
				} else {
					log.Printf("Updated content for article %d: %d chars", article.ID, len(content))
					if summarizer != nil {
						asyncSummarize(summarizer, articleRepo, article.ID, article.Title, content)
					}
				}
			} else {
				articleRepo.IncrementRefetchAttempts(article.ID)
			}
		}(&articles[i])
	}

	wg.Wait()
}

func shouldFetch(feed *model.Feed) bool {
	if feed.LastFetchedAt == nil {
		return true
	}

	elapsed := time.Since(*feed.LastFetchedAt)
	return elapsed >= time.Duration(feed.FetchIntervalMin)*time.Minute
}

func parsePublishedTime(published, updated *time.Time) *time.Time {
	if published != nil {
		return published
	}
	return updated
}
