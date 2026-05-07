package rss

import (
	"testing"
)

func TestFindMediaInBytes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantNil  bool
		wantURL  string
		wantType string
	}{
		{
			name:     "plain absolute https mp3 long basename",
			body:     `<html><body><a href="https://cdn.example.com/episode-001.mp3">play</a></body></html>`,
			wantURL:  "https://cdn.example.com/episode-001.mp3",
			wantType: "audio/mpeg",
		},
		{
			name:     "protocol-relative mp3 resolved to https",
			body:     `something "//cdn.example.com/podcast123.mp3" more`,
			wantURL:  "https://cdn.example.com/podcast123.mp3",
			wantType: "audio/mpeg",
		},
		{
			name:     "multiple candidates returns first",
			body:     `"https://cdn.example.com/firstfile.mp3" and "https://cdn.example.com/secondfile.mp3"`,
			wantURL:  "https://cdn.example.com/firstfile.mp3",
			wantType: "audio/mpeg",
		},
		{
			name:    "deny-list basename bell.mp3",
			body:    `<audio src="https://cdn.example.com/bell.mp3"></audio>`,
			wantNil: true,
		},
		{
			name:    "deny-list basename notification.mp3",
			body:    `<audio src="https://cdn.example.com/notification.mp3"></audio>`,
			wantNil: true,
		},
		{
			name:    "basename too short (under 6 chars): xx.mp3",
			body:    `<audio src="https://cdn.example.com/xx.mp3"></audio>`,
			wantNil: true,
		},
		{
			name:    "basename exactly 5 chars: abcde.mp3",
			body:    `<audio src="https://cdn.example.com/abcde.mp3"></audio>`,
			wantNil: true,
		},
		{
			name: "BBC mediaselector pattern",
			// Real BBC pattern: mediaselector/...vpid/<programme_id>.mp3
			body:     `{"urls":[{"url":"https://open.live.bbc.co.uk/mediaselector/6/redir/version/2.0/mediaset/audio-nondrm-download/proto/https/vpid/p0ng2c3n.mp3"}]}`,
			wantURL:  "https://open.live.bbc.co.uk/mediaselector/6/redir/version/2.0/mediaset/audio-nondrm-download/proto/https/vpid/p0ng2c3n.mp3",
			wantType: "audio/mpeg",
		},
		{
			name:     "m4a returns audio/mp4",
			body:     `<source src="https://cdn.example.com/episode-final.m4a" type="audio/mp4">`,
			wantURL:  "https://cdn.example.com/episode-final.m4a",
			wantType: "audio/mp4",
		},
		{
			name:     "mp4 returns video/mp4",
			body:     `<video src="https://cdn.example.com/lecture-video.mp4"></video>`,
			wantURL:  "https://cdn.example.com/lecture-video.mp4",
			wantType: "video/mp4",
		},
		{
			name:    "no audio URL in HTML",
			body:    `<html><body><p>No media here.</p></body></html>`,
			wantNil: true,
		},
		{
			name:    "empty body",
			body:    ``,
			wantNil: true,
		},
		{
			name:     "mp3 URL with query string passes basename check",
			body:     `{"url":"https://cdn.example.com/mypodcast.mp3?download=1&token=abc"}`,
			wantURL:  "https://cdn.example.com/mypodcast.mp3?download=1&token=abc",
			wantType: "audio/mpeg",
		},
		{
			name:    "deny-list chime.mp3 (case-insensitive check)",
			body:    `<audio src="https://cdn.example.com/CHIME.mp3"></audio>`,
			wantNil: true,
		},
		{
			name:     "basename exactly 6 chars passes",
			body:     `<audio src="https://cdn.example.com/abcdef.mp3"></audio>`,
			wantURL:  "https://cdn.example.com/abcdef.mp3",
			wantType: "audio/mpeg",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findMediaInBytes([]byte(tc.body), "")
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected MediaInfo, got nil (body=%q)", tc.body)
			}
			if got.URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tc.wantURL)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if got.Duration != 0 {
				t.Errorf("Duration = %d, want 0 (HTML sniff never knows duration)", got.Duration)
			}
		})
	}
}
