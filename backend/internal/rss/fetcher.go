package rss

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

type Fetcher struct {
	parser  *gofeed.Parser
	client  *http.Client
}

func NewFetcher() *Fetcher {
	// Custom client with timeout and relaxed TLS for some RSS feeds
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	return &Fetcher{
		parser: gofeed.NewParser(),
		client: client,
	}
}

type FetchResult struct {
	Feed         *gofeed.Feed
	ETag         string
	LastModified string
}

func (f *Fetcher) Fetch(ctx context.Context, url string, etag, lastModified string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set cache headers
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		// No new content
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	feed, err := f.parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}

	return &FetchResult{
		Feed:         feed,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}
