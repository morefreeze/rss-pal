// Command backfill_image_dimensions probes the intrinsic dimensions of every
// image referenced in recent article markdown and persists them to
// articles.image_dimensions. The frontend uses the map to render
// <img width=W height=H ...>, which lets the browser reserve layout space
// and prevents reading-progress regression when lazy-loaded images decode.
//
// Idempotent: --limit/--days select articles whose image_dimensions IS NULL
// or empty; re-running converges. --dry-run prints the plan without writing.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/imgmeta"
	"github.com/bytedance/rss-pal/internal/repository"
)

// mdImageRE matches the URL slot of a markdown image. Alt text is allowed to
// contain anything except ']' (the alt was already flattened by the
// scrape-time normaliser). The URL slot terminates at whitespace or ')'
// to skip over optional title strings ![alt](url "title").
var mdImageRE = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)`)

func main() {
	limit := flag.Int("limit", 20, "max articles to backfill in this run")
	days := flag.Int("days", 14, "only consider articles published or fetched within the last N days")
	concurrency := flag.Int("concurrency", 5, "concurrent image probes")
	dryRun := flag.Bool("dry-run", false, "print plan + per-image probes without writing to DB")
	flag.Parse()

	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	articles, err := selectArticles(db, *limit, *days)
	if err != nil {
		log.Fatalf("select: %v", err)
	}
	log.Printf("plan: %d articles with image_dimensions IS NULL, last %d days, capped at %d", len(articles), *days, *limit)

	prober := imgmeta.New()
	sema := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup

	for _, a := range articles {
		urls := extractImageURLs(a.Content)
		if len(urls) == 0 {
			log.Printf("article %d %q — no images, skipping", a.ID, truncate(a.Title, 60))
			continue
		}
		log.Printf("article %d %q — %d images", a.ID, truncate(a.Title, 60), len(urls))

		dims := make(map[string][2]int, len(urls))
		var mu sync.Mutex
		for _, u := range urls {
			wg.Add(1)
			sema <- struct{}{}
			go func(url string) {
				defer wg.Done()
				defer func() { <-sema }()
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				d, err := prober.Probe(ctx, url)
				if err != nil {
					log.Printf("  ✗ %s — %v", truncate(url, 80), err)
					return
				}
				log.Printf("  ✓ %s — %dx%d", truncate(url, 80), d.Width, d.Height)
				mu.Lock()
				dims[url] = [2]int{d.Width, d.Height}
				mu.Unlock()
			}(u)
		}
		wg.Wait()

		if *dryRun {
			log.Printf("  [dry-run] would persist %d/%d dimensions for article %d", len(dims), len(urls), a.ID)
			continue
		}
		// Persist whatever resolved — partial maps are still useful and we
		// don't want a single CDN failure to discard the rest.
		if err := saveDimensions(db, a.ID, dims); err != nil {
			log.Printf("  ! save article %d: %v", a.ID, err)
			continue
		}
		log.Printf("  saved %d/%d dimensions for article %d", len(dims), len(urls), a.ID)
	}
}

type articleRow struct {
	ID      int
	Title   string
	Content string
}

func selectArticles(db *sql.DB, limit, days int) ([]articleRow, error) {
	q := `
		SELECT id, title, content
		FROM articles
		WHERE image_dimensions IS NULL
		  AND content LIKE '%![%'
		  AND COALESCE(published_at, fetched_at) > NOW() - $1::interval
		ORDER BY COALESCE(published_at, fetched_at) DESC
		LIMIT $2`
	interval := fmt.Sprintf("%d days", days)
	rows, err := db.Query(q, interval, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []articleRow
	for rows.Next() {
		var a articleRow
		if err := rows.Scan(&a.ID, &a.Title, &a.Content); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func extractImageURLs(md string) []string {
	matches := mdImageRE.FindAllStringSubmatch(md, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		u := m[1]
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func saveDimensions(db *sql.DB, articleID int, dims map[string][2]int) error {
	if len(dims) == 0 {
		// Persist an empty object so we don't reconsider this article on the
		// next run — every image failed to resolve.
		return updateJSON(db, articleID, "{}")
	}
	b, err := json.Marshal(dims)
	if err != nil {
		return err
	}
	return updateJSON(db, articleID, string(b))
}

func updateJSON(db *sql.DB, articleID int, payload string) error {
	_, err := db.Exec(`UPDATE articles SET image_dimensions = $1::jsonb WHERE id = $2`, payload, articleID)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
