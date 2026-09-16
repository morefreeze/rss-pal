package main

import (
	"context"
	"database/sql"
	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"strings"
	"sync"
	"testing"
	"time"
)

type budgetLinkFetcher struct {
	mu      sync.Mutex
	owners  []int
	bounded []bool
}

func (f *budgetLinkFetcher) record(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, taskbudget.Owner(ctx))
	d, ok := ctx.Deadline()
	f.bounded = append(f.bounded, ok && time.Until(d) <= time.Minute)
}
func (f *budgetLinkFetcher) FetchHTMLDocument(ctx context.Context, _ string) (*goquery.Document, error) {
	f.record(ctx)
	return goquery.NewDocumentFromReader(strings.NewReader("<html><body><p>No links here.</p></body></html>"))
}
func (f *budgetLinkFetcher) FetchContentWithMetadata(ctx context.Context, _ string) (rss.ContentResult, error) {
	f.record(ctx)
	return rss.ContentResult{Content: "article body", Title: "fetched title"}, nil
}

func TestLinkSetBackgroundBudgetOwnsEveryArticle(t *testing.T) {
	for _, mode := range []string{"candidates", "suggestions", "children"} {
		t.Run(mode, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			oldStore, oldPolicies := workerBudgets, workerPolicies
			defer func() { workerBudgets, workerPolicies = oldStore, oldPolicies }()
			workerBudgets = taskbudget.New(db)
			workerPolicies = taskbudget.Policies{"background_fetch": {Daily: 1, GlobalDaily: 100, Concurrent: 1, GlobalConcurrent: 10, Lease: time.Minute}}
			var ownerA, ownerB int
			for _, seed := range []struct {
				name string
				id   *int
			}{{"a", &ownerA}, {"b", &ownerB}} {
				if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, seed.name).Scan(seed.id); err != nil {
					t.Fatal(err)
				}
			}
			seedArticle := func(owner int) int {
				typ := "link_set"
				if mode == "suggestions" {
					typ = "rss"
				}
				var feed, id int
				if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id,feed_type) VALUES($1,'feed',$2,$3) RETURNING id`, "https://example.test/"+string(rune('a'+owner)), owner, typ).Scan(&feed); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,processing_state) VALUES($1,'test','https://example.test/article','ready') RETURNING id`, feed).Scan(&id); err != nil {
					t.Fatal(err)
				}
				if mode == "children" {
					var child int
					if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,parent_article_id,processing_state) VALUES($1,'child','https://example.test/child',$2,'processing') RETURNING id`, feed, id).Scan(&child); err != nil {
						t.Fatal(err)
					}
					return child
				}
				return id
			}
			a, b := seedArticle(ownerA), seedArticle(ownerB)
			release, err := workerBudgets.Acquire(context.Background(), ownerA, "background_fetch", 1, workerPolicies["background_fetch"])
			if err != nil {
				t.Fatal(err)
			}
			release()
			f := &budgetLinkFetcher{}
			repo := repository.NewArticleRepository(db)
			// A misleading caller owner must not replace the persisted feed owner.
			ctx := taskbudget.WithOwner(context.Background(), ownerB)
			switch mode {
			case "candidates":
				detectLinkSetCandidates(ctx, repo, f)
			case "suggestions":
				detectLinkSetSuggestions(ctx, repo, f)
			case "children":
				processQueuedChildren(ctx, repo, f, t.TempDir())
			}
			if len(f.owners) != 1 || f.owners[0] != ownerB {
				t.Fatalf("fetch owners=%v, want only B (%d)", f.owners, ownerB)
			}
			if !f.bounded[0] {
				t.Fatal("unbounded external fetch context")
			}
			var links, suggested sql.NullBool
			var attempts int
			var state, content string
			if err := db.QueryRow(`SELECT links_extendable,link_set_suggested,COALESCE(refetch_attempts,0),processing_state,COALESCE(content,'') FROM articles WHERE id=$1`, a).Scan(&links, &suggested, &attempts, &state, &content); err != nil {
				t.Fatal(err)
			}
			if links.Valid || suggested.Valid || attempts != 0 || content != "" || (mode == "children" && state != "processing") {
				t.Fatalf("denied article changed: links=%v suggestions=%v attempts=%d state=%s content=%q", links, suggested, attempts, state, content)
			}
			var usage, leases int
			if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=$1 AND bucket='background_fetch'`, ownerB).Scan(&usage); err != nil {
				t.Fatal(err)
			}
			if usage != 1 {
				t.Fatalf("B charged %d, want 1", usage)
			}
			if err := db.QueryRow(`SELECT count(*) FROM task_budget_leases`).Scan(&leases); err != nil {
				t.Fatal(err)
			}
			if leases != 0 {
				t.Fatalf("leaked %d leases", leases)
			}
			if mode == "children" {
				if err := db.QueryRow(`SELECT content FROM articles WHERE id=$1`, b).Scan(&content); err != nil || content != "article body" {
					t.Fatalf("allowed child content=%q err=%v", content, err)
				}
			}
		})
	}
}
