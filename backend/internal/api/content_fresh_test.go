package api

import (
	"context"
	"testing"

	"github.com/bytedance/rss-pal/internal/rss"
)

type freshRecordingContentFetcher struct {
	freshCalls int
	lastURL    string
}

func (f *freshRecordingContentFetcher) FetchContentFresh(_ context.Context, target string) (string, error) {
	f.freshCalls++
	f.lastURL = target
	return "fresh Reader content", nil
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
	if content != "fresh Reader content" {
		t.Fatalf("content = %q", content)
	}
	if fetcher.freshCalls != 1 || fetcher.lastURL != "https://example.com/article" {
		t.Fatalf("fresh calls = %d, last URL = %q", fetcher.freshCalls, fetcher.lastURL)
	}
}
