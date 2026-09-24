package rss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// CheckFeed parses a bounded RSS/Atom response through the same public/derived
// platform transport boundary used by preview. It never falls back to HTML.
func (f *Fetcher) CheckFeed(ctx context.Context, rawURL string) error {
	target, client := f.resolveTarget(rawURL)
	resp, err := f.getFeedResponse(ctx, client, target, func(r *http.Request) { r.Header.Set("User-Agent", userAgent) })
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("RSS HTTP status %d", resp.StatusCode)
	}
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return fmt.Errorf("RSS response exceeds 2 MiB")
	}
	feed, err := f.parser.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("RSS parse failed: %w", err)
	}
	if feed == nil || len(feed.Items) == 0 {
		return fmt.Errorf("RSS has no articles")
	}
	return nil
}
