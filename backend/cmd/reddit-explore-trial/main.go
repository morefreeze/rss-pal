// reddit-explore-trial reads the same bounded listings and validates sources
// with the production parser/fetcher. It never opens a database or subscribes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
)

type listingResult struct {
	Subreddit string                  `json:"subreddit"`
	Period    string                  `json:"period"`
	Endpoint  string                  `json:"endpoint"`
	Stats     *explore.RedditTopStats `json:"stats,omitempty"`
	Error     string                  `json:"error,omitempty"`
}
type sourceResult struct {
	URL      string `json:"url"`
	FeedURL  string `json:"feed_url,omitempty"`
	Articles int    `json:"articles,omitempty"`
	Error    string `json:"error,omitempty"`
}
type report struct {
	GeneratedAt        time.Time       `json:"generated_at"`
	MinScore           int             `json:"min_score"`
	LimitPerListing    int             `json:"limit_per_listing"`
	Listings           []listingResult `json:"listings"`
	SuccessfulListings int             `json:"successful_listings"`
	QualifiedPosts     int             `json:"unique_qualified_posts"`
	ExternalPosts      int             `json:"unique_external_posts"`
	UniqueSites        int             `json:"unique_external_hosts"`
	Sources            []sourceResult  `json:"sources"`
	ValidFeeds         int             `json:"unique_valid_feeds"`
	ValidationEnabled  bool            `json:"validation_enabled"`
}

func main() {
	minimum := flag.Int("min-score", explore.DefaultRedditMinScore, "positive Reddit score threshold")
	input := flag.String("input-dir", "", "read saved <subreddit>-<period>.json instead of networking")
	save := flag.String("save-dir", "", "save successful raw public listings for repeatable trials")
	validate := flag.Bool("validate", true, "run safe RSS discovery and freshness/content validation")
	flag.Parse()
	if *minimum < 1 {
		fmt.Fprintln(os.Stderr, "min-score must be positive")
		os.Exit(2)
	}
	if *save != "" {
		if err := os.MkdirAll(*save, 0700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	ctx := context.Background()
	r := report{GeneratedAt: time.Now().UTC(), MinScore: *minimum, LimitPerListing: 100, ValidationEnabled: *validate, Sources: []sourceResult{}}
	client := explore.NewProviderClient("")
	candidates := map[string]explore.Candidate{}
	qualified := map[string]bool{}
	external := map[string]bool{}
	hosts := map[string]bool{}
	for _, board := range []string{"programming", "MachineLearning", "LocalLLaMA", "artificial"} {
		for _, period := range []string{"week", "month"} {
			endpoint := explore.RedditTopEndpoint(board, period, *minimum)
			result := listingResult{Subreddit: board, Period: period, Endpoint: endpoint}
			filename := board + "-" + period + ".json"
			var body []byte
			var err error
			if *input != "" {
				body, err = os.ReadFile(filepath.Join(*input, filename))
			} else {
				fetched, fetchErr := client.Fetch(ctx, endpoint, "", "")
				body, err = fetched.Body, fetchErr
			}
			if err == nil {
				parsed, stats, parseErr := explore.ParseRedditTop(explore.Provider{Endpoint: endpoint, Topic: board}, body)
				err = parseErr
				if err == nil {
					result.Stats = &stats
					r.SuccessfulListings++
					// Count qualifying IDs across overlapping week/month lists exactly once.
					for _, id := range stats.QualifiedPostIDs {
						qualified[id] = true
					}
					for _, id := range stats.ExternalPostIDs {
						external[id] = true
					}
					for _, c := range parsed {
						candidates[c.FeedURL] = c
						u, _ := url.Parse(c.FeedURL)
						hosts[u.Hostname()] = true
					}
					if *save != "" {
						if saveErr := os.WriteFile(filepath.Join(*save, filename), body, 0600); saveErr != nil {
							fmt.Fprintln(os.Stderr, saveErr)
							os.Exit(2)
						}
					}
				}
			}
			if err != nil {
				result.Error = err.Error()
			}
			r.Listings = append(r.Listings, result)
		}
	}
	r.QualifiedPosts = len(qualified)
	r.ExternalPosts = len(external)
	r.UniqueSites = len(hosts)
	if *validate {
		jobs := make(chan string)
		results := make(chan sourceResult, len(candidates))
		var workers sync.WaitGroup
		for i := 0; i < 4; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				fetcher := explore.NewSourceFetcher()
				for raw := range jobs {
					now := time.Now()
					result, err := fetcher.Fetch(ctx, explore.SourceFetchRequest{URL: raw, Mode: explore.SourceFetchValidate, Evidence: []explore.ObservationEvidence{{ProviderID: 1, ProviderKind: "reddit_top", Enabled: true, LastSeenAt: now, ProviderLastSuccessAt: &now, OccurrenceCount: 1}}})
					source := sourceResult{URL: raw, FeedURL: result.FeedURL, Articles: len(result.Articles)}
					if err != nil {
						source.Error = err.Error()
					}
					results <- source
				}
			}()
		}
		for raw := range candidates {
			jobs <- raw
		}
		close(jobs)
		workers.Wait()
		close(results)
		feeds := map[string]bool{}
		for source := range results {
			r.Sources = append(r.Sources, source)
			if source.Error == "" {
				feeds[source.FeedURL] = true
			}
		}
		sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].URL < r.Sources[j].URL })
		r.ValidFeeds = len(feeds)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if r.SuccessfulListings != len(r.Listings) {
		os.Exit(1)
	}
}
