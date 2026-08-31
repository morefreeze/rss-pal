package explore

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCandidateCollectorBoundsAndMergesIndependentlyOfInputOrder(t *testing.T) {
	forward := make([]Candidate, 0, 2202)
	for i := 2201; i >= 0; i-- {
		forward = append(forward, Candidate{
			ExternalKey:     fmt.Sprintf("key-%04d", i),
			FeedURL:         fmt.Sprintf("https://feeds.example/%04d", i),
			SiteURL:         fmt.Sprintf("https://site.example/%04d", i),
			Title:           fmt.Sprintf("title-%04d", i),
			Topic:           fmt.Sprintf("topic-%04d", i),
			Tags:            []string{fmt.Sprintf("tag-%04d", i)},
			OccurrenceCount: 1,
		})
	}
	forward = append(forward,
		Candidate{ExternalKey: "z", FeedURL: "https://feeds.example/0000", Title: "z", Tags: []string{"z"}, OccurrenceCount: 3},
		Candidate{ExternalKey: "a", FeedURL: "https://feeds.example/0000", SiteURL: "https://site.example/a", Title: "a", Topic: "a", Tags: []string{"a"}, OccurrenceCount: 4},
	)
	reverse := append([]Candidate(nil), forward...)
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}

	collect := func(input []Candidate) []Candidate {
		collector := newCandidateCollector(2000, func(candidate Candidate) string { return candidate.FeedURL })
		for _, candidate := range input {
			collector.add(candidate)
			if len(collector.items) > 2000 {
				t.Fatalf("collector retained %d keys", len(collector.items))
			}
		}
		return collector.candidates()
	}
	got, reversed := collect(forward), collect(reverse)
	if len(got) != 2000 || got[0].FeedURL != "https://feeds.example/0000" || got[len(got)-1].FeedURL != "https://feeds.example/1999" {
		t.Fatalf("bounded keys = %d, first/last = %#v / %#v", len(got), got[0], got[len(got)-1])
	}
	if !reflect.DeepEqual(got, reversed) {
		t.Fatalf("collector depends on input order:\nforward=%#v\nreverse=%#v", got[:2], reversed[:2])
	}
	if got[0].OccurrenceCount != 8 || !reflect.DeepEqual(got[0].Tags, []string{"a", "tag-0000", "z"}) || got[0].ExternalKey != "a" || got[0].Title != "a" || got[0].SiteURL != "https://site.example/a" || got[0].Topic != "a" {
		t.Fatalf("merged candidate = %#v", got[0])
	}
}
