package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/backup"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/imagefetch"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/transcript"
	"github.com/mmcdole/gofeed"
)

var cycleMu sync.Mutex

const (
	maxConcurrentFeeds            = 5
	maxConcurrentContent          = 3
	maxConcurrentSummary          = 2
	feedTimeout                   = 3 * time.Minute
	maxRefetchPerCycle            = 20
	maxNewArticlesPerFeed         = 10
	maxBackfillPerCycle           = 5
	maxTranscriptBackfillPerCycle = 5
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
	articleRepo.SetImageBaseDir(cfg.Backup.Dir)
	prefRepo := repository.NewPreferenceRepository(db)
	userRepo := repository.NewUserRepository(db)
	templateRepo := repository.NewTemplateRepository(db)
	userInsightsRepo := repository.NewUserInsightRepository(db)

	fetcher := rss.NewFetcher(cfg.RSSHub.BaseURL)
	contentFetcher := rss.NewContentFetcher()

	transcriptFetcher := &transcript.MultiFetcher{
		Strategies: []transcript.Fetcher{
			&transcript.YouTubeCC{},
			&transcript.BilibiliCC{},
			&transcript.HTMLPageScraper{Docs: contentFetcher},
		},
	}

	var summarizer *ai.Summarizer
	if cfg.Claude.APIKey != "" {
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
		summarizer.SetVisionModel(cfg.AI.Vision.Model)
		log.Println("AI summarizer initialized")
	} else {
		log.Println("CLAUDE_API_KEY not set, AI summarization disabled")
	}

	if summarizer != nil {
		stopCron := scheduleDailyInsightCron(insightCronDeps{
			userRepo:         userRepo,
			prefRepo:         prefRepo,
			articleRepo:      articleRepo,
			userInsightsRepo: userInsightsRepo,
			templateRepo:     templateRepo,
			summarizer:       summarizer,
			defaultModel:     ai.DefaultModel,
		})
		defer stopCron()
	}

	backupRunner := backup.NewRunner(db, cfg.Backup.Dir)
	stopBackup := backupRunner.ScheduleDaily(context.Background())
	defer stopBackup()

	// Async PDF OCR loop: runs every 60s, drains up to maxPDFOCRPerCycle
	// scanned-PDF clip articles per tick. Lives in its own goroutine so a
	// long Tesseract pass on one PDF doesn't delay the main feed-fetch
	// cycle (and vice versa).
	pdfOCRCtx, cancelPDFOCR := context.WithCancel(context.Background())
	defer cancelPDFOCR()
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pdfOCRCtx.Done():
				return
			case <-t.C:
				processPDFOCR(pdfOCRCtx, articleRepo, *cfg)
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	runFetchCycle(context.Background(), cfg, feedRepo, articleRepo, prefRepo, fetcher, contentFetcher, summarizer, transcriptFetcher, cfg.Backup.Dir)

	for range ticker.C {
		runFetchCycle(context.Background(), cfg, feedRepo, articleRepo, prefRepo, fetcher, contentFetcher, summarizer, transcriptFetcher, cfg.Backup.Dir)
	}
}

func runFetchCycle(ctx context.Context, cfg *config.Config, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, transcriptFetcher transcript.Fetcher, imageBaseDir string) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	detectLinkSetCandidates(ctx, articleRepo, contentFetcher)
	detectLinkSetSuggestions(ctx, articleRepo, contentFetcher)
	processQueuedChildren(ctx, articleRepo, contentFetcher, imageBaseDir)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if transcriptFetcher != nil {
		backfillTranscripts(ctx, articleRepo, transcriptFetcher)
	}
	if summarizer != nil {
		backfillSummaries(ctx, cfg, articleRepo, summarizer)
		runClassifyCycle(ctx, articleRepo, prefRepo, summarizer)
	}
}

func asyncSummarize(summarizer *ai.Summarizer, articleRepo *repository.ArticleRepository, articleID int, title, content string) {
	go func() {
		sumSem <- struct{}{}
		defer func() { <-sumSem }()
		sCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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

func backfillSummaries(ctx context.Context, cfg *config.Config, articleRepo *repository.ArticleRepository, summarizer *ai.Summarizer) {
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
			sCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
			defer cancel()

			var result *ai.SummaryResult
			var err error

			visionCfg := cfg.AI.Vision
			if ai.ShouldUseVisionAuto(article.Content, visionCfg) {
				urls := ai.ExtractImageURLs(article.Content)
				urls = filterCandidateImageURLs(urls, visionCfg.MaxImages)
				if len(urls) > 0 {
					ifCfg := imagefetch.Config{
						Dir:                   visionCfg.CacheDir,
						LocalArticleImagesDir: filepath.Join(cfg.Backup.Dir, "article_images"),
						MaxLongSide:           visionCfg.MaxLongSide,
						TTL:                   visionCfg.CacheTTL,
					}
					paths, _ := imagefetch.FetchAndStore(sCtx, article.ID, urls, ifCfg)
					if len(paths) > 0 {
						log.Printf("Vision-summarizing article %d with %d images", article.ID, len(paths))
						result, err = summarizer.SummarizeWithImages(sCtx, article.Title, article.Content, paths)
					}
				}
			}
			if result == nil {
				result, err = summarizer.Summarize(sCtx, article.Title, article.Content)
			}
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

// filterCandidateImageURLs drops avatar / unsupported / out-of-budget URLs.
// Local /api/articles/<id>/images/<idx>.<ext> URLs pass through unchanged —
// imagefetch resolves them to on-disk paths without downloading.
func filterCandidateImageURLs(urls []string, maxImages int) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if rss.IsAvatarImageURL(u, "") {
			continue
		}
		if isAcceptableImageURL(u) {
			out = append(out, u)
		}
		if len(out) >= maxImages {
			break
		}
	}
	return out
}

func isAcceptableImageURL(u string) bool {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return true
	}
	if strings.HasPrefix(u, "/api/articles/") && strings.Contains(u, "/images/") {
		return true
	}
	return false
}

func backfillTranscripts(ctx context.Context, articleRepo *repository.ArticleRepository, fetcher transcript.Fetcher) {
	articles, err := articleRepo.GetMediaArticlesWithoutTranscript(maxTranscriptBackfillPerCycle)
	if err != nil {
		log.Printf("Failed to get media articles without transcript: %v", err)
		return
	}
	if len(articles) == 0 {
		return
	}
	log.Printf("Fetching transcripts for %d media articles", len(articles))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentContent)
	for i := range articles {
		a := &articles[i]
		wg.Add(1)
		go func(article *model.Article) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			result, err := fetcher.Fetch(tCtx, article)
			if err != nil {
				log.Printf("Transcript fetch error for article %d: %v", article.ID, err)
				return // leave transcript_fetched_at NULL → retried next cycle
			}
			if result == nil || strings.TrimSpace(result.Text) == "" {
				if err := articleRepo.MarkTranscriptFetchAttempted(article.ID); err != nil {
					log.Printf("Failed to mark transcript attempt for article %d: %v", article.ID, err)
				}
				return
			}
			newContent := buildContentWithTranscript(article.Content, result)
			wc, rm := rss.ComputeMetrics(newContent)
			if err := articleRepo.UpdateContentAndResetSummary(article.ID, newContent, wc, rm); err != nil {
				log.Printf("Failed to save transcript for article %d: %v", article.ID, err)
				return
			}
			log.Printf("Transcript fetched for article %d (source=%s, %d chars)", article.ID, result.Source, len(result.Text))
		}(a)
	}
	wg.Wait()
}

// buildContentWithTranscript appends the transcript to existing article
// content using the markdown separator pattern documented in the spec.
func buildContentWithTranscript(existing string, r *transcript.Result) string {
	existing = strings.TrimSpace(existing)
	var b strings.Builder
	if existing != "" {
		b.WriteString(existing)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString("## 字幕\n\n")
	if r.Source != "" {
		b.WriteString("> 来源：")
		b.WriteString(r.Source)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(r.Text))
	return b.String()
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
		mediaInfo := rss.ExtractVideoMedia(item.Link)
		if mediaInfo == nil {
			mediaInfo = rss.ExtractMedia(item)
		}
		if exists {
			articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, parsePublishedTime(item.PublishedParsed, item.UpdatedParsed))
			if mediaInfo != nil {
				if err := articleRepo.UpdateMediaIfNull(feed.ID, item.Link, mediaInfo.URL, mediaInfo.Type, mediaInfo.Duration); err != nil {
					log.Printf("Failed to backfill media for %s: %v", item.Link, err)
				}
			}
			continue
		}

		queuedCount++

		wg.Add(1)
		go func(item *gofeed.Item, mediaInfo *rss.MediaInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content := rss.StripHTML(item.Description)
			if content == "" {
				content = rss.StripHTML(item.Content)
			}

			// Skip deep-fetch for video articles too — the watch page is
			// JS-heavy and the scraped content is unusable. The transcript
			// pipeline (backfillTranscripts) is the right path for these.
			skipDeepFetch := feed.FeedType == "youtube" || feed.FeedType == "podcast"
			if mediaInfo != nil && strings.HasPrefix(mediaInfo.Type, "video/") {
				skipDeepFetch = true
			}
			if !skipDeepFetch && item.Link != "" {
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
			if mediaInfo != nil {
				article.MediaURL = mediaInfo.URL
				article.MediaType = mediaInfo.Type
				article.MediaDurationSeconds = mediaInfo.Duration
				// If this is a video and the body also mentions the same video,
				// strip the in-body placeholder so it isn't rendered twice.
				if strings.HasPrefix(mediaInfo.Type, "video/") {
					if v, ok := rss.ParseEmbedURL(mediaInfo.URL); ok {
						article.Content = rss.StripDuplicateVideo(article.Content, v)
					}
				}
			}

			if err := articleRepo.Create(article); err != nil {
				log.Printf("Failed to create article: %v", err)
			} else {
				atomic.AddInt64(&newCount, 1)
				if summarizer != nil {
					asyncSummarize(summarizer, articleRepo, article.ID, article.Title, article.Content)
				}
			}
		}(item, mediaInfo)
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
		// Twitter / X captures come from the bookmarklet's tweet-aware
		// parser; the short-content backfill (which calls Direct → Jina)
		// would clobber a byline-only image-only capture with an unrelated
		// Jina-extracted article, losing structure and ruining the title.
		// Better to leave the byline as-is than overwrite with garbage.
		if _, ok := rss.IsTwitterStatusURL(articles[i].URL); ok {
			log.Printf("refetchShortContent: skipping Twitter status URL article=%d url=%s", articles[i].ID, articles[i].URL)
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
