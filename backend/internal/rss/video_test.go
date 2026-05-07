package rss

import "testing"

func TestExtractVideo_YouTube(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantID    string
		wantStart int
	}{
		{"watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"watch_with_t", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "dQw4w9WgXcQ", 42},
		{"watch_with_t_no_s", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=90", "dQw4w9WgXcQ", 90},
		{"short", "https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"short_with_hash_t", "https://youtu.be/dQw4w9WgXcQ?t=15", "dQw4w9WgXcQ", 15},
		{"embed", "https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"watch_with_list_ignored", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc", "dQw4w9WgXcQ", 0},
		{"nocookie_embed", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"uppercase_host", "https://WWW.YouTube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractVideo(tc.in)
			if !ok {
				t.Fatalf("ExtractVideo(%q) returned ok=false", tc.in)
			}
			if got.Platform != "youtube" {
				t.Errorf("Platform = %q, want youtube", got.Platform)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tc.wantID)
			}
			if got.Start != tc.wantStart {
				t.Errorf("Start = %d, want %d", got.Start, tc.wantStart)
			}
		})
	}
}

func TestExtractVideo_NotAVideo(t *testing.T) {
	cases := []string{
		"",
		"https://example.com/post/123",
		"https://vimeo.com/123456",
		"https://www.youtube.com/",
		"https://www.youtube.com/watch",                  // no v=
		"https://www.youtube.com/watch?v=tooShort",       // wrong length
		"https://www.youtube.com/feed/subscriptions",
		"https://b23.tv/abc123",                          // out of scope
		"https://www.bilibili.com/video/av12345",         // legacy AV out of scope
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got, ok := ExtractVideo(in); ok {
				t.Errorf("ExtractVideo(%q) = %+v, want ok=false", in, got)
			}
		})
	}
}
