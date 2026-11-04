package api

import (
	"testing"
	"time"
)

func TestFeedPublishedTimeSkipsZeroDate(t *testing.T) {
	zero := time.Time{}
	if got := publishedTime(&zero, &zero); got != nil {
		t.Fatalf("zero publication date will be persisted: %v", got)
	}
	valid := time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)
	if got := publishedTime(&valid, nil); got != &valid {
		t.Fatalf("valid publication date changed: %v", got)
	}
	if got := publishedTime(&zero, &valid); got != &valid {
		t.Fatalf("valid update fallback lost: %v", got)
	}
}
