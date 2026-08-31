package config

import "testing"

func TestLoadExploreConfigValidation(t *testing.T) {
	cases := []struct {
		name          string
		batch, concur string
		wantBatch     int
		wantConcur    int
	}{
		{name: "unset", wantBatch: 500, wantConcur: 5},
		{name: "malformed", batch: "nope", concur: "bad", wantBatch: 500, wantConcur: 5},
		{name: "zero", batch: "0", concur: "0", wantBatch: 500, wantConcur: 5},
		{name: "negative", batch: "-1", concur: "-2", wantBatch: 500, wantConcur: 5},
		{name: "one", batch: "1", concur: "1", wantBatch: 1, wantConcur: 1},
		{name: "499", batch: "499", concur: "7", wantBatch: 499, wantConcur: 7},
		{name: "500", batch: "500", concur: "9", wantBatch: 500, wantConcur: 9},
		{name: "501", batch: "501", concur: "3", wantBatch: 500, wantConcur: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EXPLORE_FETCH_BATCH_LIMIT", tc.batch)
			t.Setenv("EXPLORE_FETCH_CONCURRENCY", tc.concur)
			got := Load().Explore
			if got.FetchBatchLimit != tc.wantBatch || got.FetchConcurrency != tc.wantConcur {
				t.Fatalf("Explore = %+v, want batch=%d concurrency=%d", got, tc.wantBatch, tc.wantConcur)
			}
		})
	}
}
