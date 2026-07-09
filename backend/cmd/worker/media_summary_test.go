package main

import "testing"

func TestShouldAsyncSummarizeAfterCreateWaitsForMediaTranscript(t *testing.T) {
	cases := []struct {
		name      string
		mediaType string
		want      bool
	}{
		{"plain article", "", true},
		{"image enclosure is not transcript-backed", "image/jpeg", true},
		{"youtube video waits for transcript backfill", "video/youtube", false},
		{"bilibili video waits for transcript backfill", "video/bilibili", false},
		{"podcast audio waits for transcript backfill", "audio/mpeg", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAsyncSummarizeAfterCreate(tc.mediaType); got != tc.want {
				t.Fatalf("shouldAsyncSummarizeAfterCreate(%q) = %v, want %v", tc.mediaType, got, tc.want)
			}
		})
	}
}
