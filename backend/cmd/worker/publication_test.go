package main

import (
	"testing"
	"time"
)

func TestWorkerPublishedTimeSkipsZeroDate(t *testing.T) {
	zero := time.Time{}
	if got := parsePublishedTime(&zero, &zero); got != nil {
		t.Fatalf("zero publication date will be persisted: %v", got)
	}
	valid := time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)
	if got := parsePublishedTime(&valid, nil); got != &valid {
		t.Fatalf("valid publication date changed: %v", got)
	}
	if got := parsePublishedTime(&zero, &valid); got != &valid {
		t.Fatalf("valid update fallback lost: %v", got)
	}
}
