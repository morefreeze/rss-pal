// Command backfill_content re-fetches every article that already has content
// (i.e. previously scraped under the plain-text era) and overwrites it with
// the new markdown output. Idempotent: re-running converges. Rate-limited via
// --qps so it does not trip source-side rate limiters.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

func main() {
	qps := flag.Float64("qps", 1.0, "max requests per second to source sites")
	feedID := flag.Int("feed-id", 0, "limit to one feed id (0 = all feeds)")
	dryRun := flag.Bool("dry-run", false, "log work without writing to DB")
	flag.Parse()

	cfg := config.Load()
	db, err := repository.NewBypassDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	rows, err := selectArticles(db, *feedID)
	if err != nil {
		log.Fatalf("select: %v", err)
	}
	defer rows.Close()

	articleRepo := repository.NewArticleRepository(db)
	fetcher := rss.NewContentFetcher()

	interval := time.Duration(float64(time.Second) / *qps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	type job struct {
		ID  int
		URL string
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.ID, &j.URL); err != nil {
			log.Fatalf("scan: %v", err)
		}
		jobs = append(jobs, j)
	}
	total := len(jobs)
	log.Printf("backfill: %d articles queued (qps=%.2f, dryRun=%v)", total, *qps, *dryRun)

	ok, fail := 0, 0
	for i, j := range jobs {
		<-ticker.C
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		md, err := fetcher.FetchContent(ctx, j.URL)
		cancel()
		if err != nil || md == "" {
			fail++
			log.Printf("[%d/%d] ✗ id=%d url=%s err=%v", i+1, total, j.ID, j.URL, err)
			continue
		}
		if *dryRun {
			ok++
			log.Printf("[%d/%d] ✓ id=%d (dry-run, %d chars)", i+1, total, j.ID, len(md))
			continue
		}
		wc, rm := rss.ComputeMetrics(md)
		if err := articleRepo.UpdateContent(j.ID, md, wc, rm); err != nil {
			fail++
			log.Printf("[%d/%d] ✗ id=%d update: %v", i+1, total, j.ID, err)
			continue
		}
		ok++
		log.Printf("[%d/%d] ✓ id=%d (%d chars)", i+1, total, j.ID, len(md))
	}
	log.Printf("backfill done: ok=%d fail=%d total=%d", ok, fail, total)
}

func selectArticles(db *sql.DB, feedID int) (*sql.Rows, error) {
	if feedID > 0 {
		return db.Query(
			`SELECT id, url FROM articles WHERE feed_id=$1 AND content IS NOT NULL AND content != '' ORDER BY id`,
			feedID,
		)
	}
	return db.Query(
		`SELECT id, url FROM articles WHERE content IS NOT NULL AND content != '' ORDER BY id`,
	)
}
