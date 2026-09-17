package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/bytedance/rss-pal/internal/feedcatalog"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"log"
	"sync"
	"time"
)

// initializeFeedCatalog is a one-time bounded seed check. It never touches feeds,
// runs normal safe validation and admission, and skips anything edited by an admin.
func initializeFeedCatalog(db *sql.DB, base string, budgets *taskbudget.Store, policies taskbudget.Policies) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	var actor int
	if err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE is_admin ORDER BY id LIMIT 1`).Scan(&actor); err != nil {
		return err
	}
	repo := repository.NewFeedCatalogRepository(db)
	s := feedcatalog.NewFeedCatalogService(repo, feedcatalog.CatalogRSSVerifier(base))
	entries, err := repo.List(ctx, false)
	if err != nil {
		return err
	}
	jobs := make(chan repository.FeedCatalogEntry)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				release, err := budgets.Acquire(ctx, actor, "interactive", 1, policies["interactive"])
				if err == nil {
					var fetchRelease func()
					fetchRelease, err = budgets.Acquire(ctx, actor, "fetch", 1, policies["fetch"])
					if err == nil {
						var result repository.FeedCatalogEntry
						result, err = s.Check(ctx, e.ID, actor, e.Revision, true)
						log.Printf("catalog seed id=%d status=%s published=%t", e.ID, result.CheckStatus, result.Published)
						fetchRelease()
					}
					release()
				}
				if err != nil {
					mu.Lock()
					failures = append(failures, fmt.Errorf("entry %d: %w", e.ID, err))
					mu.Unlock()
				}
			}
		}()
	}
	for _, e := range entries {
		if e.SeedKey != nil && e.Revision == 1 && !e.Published && e.CheckStatus == "unchecked" {
			select {
			case jobs <- e:
			case <-ctx.Done():
			}
		}
	}
	close(jobs)
	wg.Wait()
	log.Printf("catalog initialization finished: %d entries could not be published (left as drafts)", len(failures))
	return ctx.Err()
}
