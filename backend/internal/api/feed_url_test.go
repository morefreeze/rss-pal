package api

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeFeedURLCanonicalizesUppercaseHTTPS(t *testing.T) {
	got, err := normalizeFeedURL("  HTTPS://source.example/post  ")
	if err != nil {
		t.Fatalf("normalizeFeedURL() error = %v", err)
	}
	if got != "https://source.example/post" {
		t.Fatalf("normalizeFeedURL() = %q", got)
	}
}

func TestNormalizeFeedURLRejectsInternalSourceSchemes(t *testing.T) {
	for _, raw := range []string{"extension://twitter:user/1/alice", "bookmarklet://user/1", "https://extension://twitter:user/1/alice", "https://bookmarklet://user/1"} {
		if normalized, err := normalizeFeedURL(raw); err == nil {
			t.Errorf("accepted internal source %q as %q", raw, normalized)
		}
	}
}

func TestFeedHandlerUsesPublicFetcher(t *testing.T) {
	handler := NewFeedHandler(nil, nil, "http://rsshub.internal:1200")
	_, err := handler.fetcher.Fetch(context.Background(), "http://127.0.0.1:8080/feed", "", "")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("FeedHandler fetch error = %v, want blocked address", err)
	}
}

func TestValidatePublicURLRejectsCredentials(t *testing.T) {
	if err := validatePublicURL("https://user:secret@source.example/feed"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("validatePublicURL() error = %v, want credentials rejection", err)
	}
}
