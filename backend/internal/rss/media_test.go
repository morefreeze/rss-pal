package rss

import (
	"testing"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
)

func TestExtractMedia_AudioEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/ep1.mp3", Type: "audio/mpeg", Length: "31415926"},
		},
	}
	got := ExtractMedia(item)
	if got == nil {
		t.Fatal("expected MediaInfo, got nil")
	}
	if got.URL != "https://cdn.example.com/ep1.mp3" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Type != "audio/mpeg" {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Duration != 0 {
		t.Errorf("Duration without itunes ext should be 0, got %d", got.Duration)
	}
}

func TestExtractMedia_PrefersAudioOverImage(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/cover.jpg", Type: "image/jpeg"},
			{URL: "https://cdn.example.com/ep1.mp3", Type: "audio/mpeg"},
		},
	}
	got := ExtractMedia(item)
	if got == nil || got.URL != "https://cdn.example.com/ep1.mp3" {
		t.Fatalf("expected audio URL, got %+v", got)
	}
}

func TestExtractMedia_VideoEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/ep1.mp4", Type: "video/mp4"},
		},
	}
	got := ExtractMedia(item)
	if got == nil || got.Type != "video/mp4" {
		t.Fatalf("expected video, got %+v", got)
	}
}

func TestExtractMedia_NoEnclosure(t *testing.T) {
	if got := ExtractMedia(&gofeed.Item{}); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestExtractMedia_OnlyImageEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/cover.jpg", Type: "image/jpeg"},
		},
	}
	if got := ExtractMedia(item); got != nil {
		t.Fatalf("expected nil for image-only enclosures, got %+v", got)
	}
}

func TestExtractMedia_DurationFromITunes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"hh:mm:ss", "01:02:03", 3723},
		{"mm:ss", "42:13", 2533},
		{"raw seconds", "2530", 2530},
		{"empty", "", 0},
		{"invalid", "garbage", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{URL: "https://x/ep.mp3", Type: "audio/mpeg"},
				},
				ITunesExt: &ext.ITunesItemExtension{Duration: tc.raw},
			}
			got := ExtractMedia(item)
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Duration != tc.want {
				t.Errorf("Duration = %d, want %d (raw=%q)", got.Duration, tc.want, tc.raw)
			}
		})
	}
}
