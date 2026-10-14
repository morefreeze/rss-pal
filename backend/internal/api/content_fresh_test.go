package api

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/rss"
)

type freshRecordingContentFetcher struct {
	freshCalls  int
	lastURL     string
	publishedAt *time.Time
}

func (f *freshRecordingContentFetcher) FetchContentFreshWithMetadata(_ context.Context, target string) (rss.ContentResult, error) {
	f.freshCalls++
	f.lastURL = target
	return rss.ContentResult{Content: "fresh Reader content", PublishedAt: f.publishedAt}, nil
}

func (f *freshRecordingContentFetcher) FindMediaInHTML(context.Context, string) *rss.MediaInfo {
	return nil
}

func TestContentHandlerFetchFreshContentUsesFreshFetcher(t *testing.T) {
	fetcher := &freshRecordingContentFetcher{}
	handler := &ContentHandler{contentFetcher: fetcher}

	content, err := handler.fetchFreshContent(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("fetchFreshContent: %v", err)
	}
	if content.Content != "fresh Reader content" {
		t.Fatalf("content = %+v", content)
	}
	if fetcher.freshCalls != 1 || fetcher.lastURL != "https://example.com/article" {
		t.Fatalf("fresh calls = %d, last URL = %q", fetcher.freshCalls, fetcher.lastURL)
	}
}
