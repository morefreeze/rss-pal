package api

import (
	"strings"
	"testing"
)

func TestValidateBatchFetchCandidatesNormalizesAndDedupes(t *testing.T) {
	got, err := validateBatchFetchCandidates([]BatchFetchCandidate{
		{Title: "  First  ", URL: "https://Example.com/a/?utm_source=x"},
		{Title: "duplicate", URL: "https://example.com/a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://example.com/a" || got[0].Title != "First" {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestValidateBatchFetchCandidatesFallsBackToURLTitle(t *testing.T) {
	got, err := validateBatchFetchCandidates([]BatchFetchCandidate{{URL: "https://example.com/a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "https://example.com/a" {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestValidateBatchFetchCandidatesRejectsWholeBatch(t *testing.T) {
	tooMany := make([]BatchFetchCandidate, 101)
	for i := range tooMany {
		tooMany[i] = BatchFetchCandidate{URL: "https://example.com/" + strings.Repeat("a", i+1)}
	}

	tests := []struct {
		name  string
		input []BatchFetchCandidate
	}{
		{name: "empty"},
		{name: "too many", input: tooMany},
		{name: "mailto", input: []BatchFetchCandidate{{Title: "mail", URL: "mailto:a@b.com"}}},
		{name: "missing host", input: []BatchFetchCandidate{{Title: "bad", URL: "https:///x"}}},
		{name: "long URL", input: []BatchFetchCandidate{{Title: "bad", URL: "https://example.com/" + strings.Repeat("a", 4097)}}},
		{name: "long title", input: []BatchFetchCandidate{{Title: strings.Repeat("标", 501), URL: "https://example.com/a"}}},
		{name: "long note", input: []BatchFetchCandidate{{Title: "bad", URL: "https://example.com/a", EditorNote: strings.Repeat("注", 2001)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateBatchFetchCandidates(tt.input); err == nil {
				t.Fatalf("expected error for %+v", tt.input)
			}
		})
	}
}
