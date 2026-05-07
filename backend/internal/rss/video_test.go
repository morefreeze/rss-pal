package rss

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

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

func TestExtractVideo_Bilibili(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantID    string
		wantPage  int
		wantStart int
	}{
		{"plain_bv", "https://www.bilibili.com/video/BV1xx411c7mD", "BV1xx411c7mD", 0, 0},
		{"trailing_slash", "https://www.bilibili.com/video/BV1xx411c7mD/", "BV1xx411c7mD", 0, 0},
		{"with_page", "https://www.bilibili.com/video/BV1xx411c7mD/?p=2", "BV1xx411c7mD", 2, 0},
		{"with_t", "https://www.bilibili.com/video/BV1xx411c7mD?t=15", "BV1xx411c7mD", 0, 15},
		{"with_page_and_t", "https://www.bilibili.com/video/BV1xx411c7mD?p=3&t=42", "BV1xx411c7mD", 3, 42},
		{"m_subdomain", "https://m.bilibili.com/video/BV1xx411c7mD", "BV1xx411c7mD", 0, 0},
		{"player_embed", "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=2", "BV1xx411c7mD", 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractVideo(tc.in)
			if !ok {
				t.Fatalf("ExtractVideo(%q) returned ok=false", tc.in)
			}
			if got.Platform != "bilibili" {
				t.Errorf("Platform = %q, want bilibili", got.Platform)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tc.wantID)
			}
			if got.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", got.Page, tc.wantPage)
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

func TestVideoEmbed_Placeholder(t *testing.T) {
	cases := []struct {
		name string
		v    VideoEmbed
		want string
	}{
		{"yt_basic", VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ"}, "[[video:youtube:dQw4w9WgXcQ]]"},
		{"yt_start", VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ", Start: 42}, "[[video:youtube:dQw4w9WgXcQ?start=42]]"},
		{"bili_basic", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD"}, "[[video:bilibili:BV1xx411c7mD]]"},
		{"bili_page", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2}, "[[video:bilibili:BV1xx411c7mD?page=2]]"},
		{"bili_page_start", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2, Start: 15}, "[[video:bilibili:BV1xx411c7mD?page=2&start=15]]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.v.Placeholder(); got != tc.want {
				t.Errorf("Placeholder() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEmbedURL(t *testing.T) {
	cases := []struct {
		in   string
		want VideoEmbed // EmbedURL ignored
	}{
		{
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
			VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ"},
		},
		{
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0&start=42",
			VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ", Start: 42},
		},
		{
			"https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&high_quality=1&autoplay=0&page=1",
			VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 1},
		},
		{
			"https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&high_quality=1&autoplay=0&page=2&t=15",
			VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2, Start: 15},
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseEmbedURL(tc.in)
			if !ok {
				t.Fatalf("ParseEmbedURL(%q) ok=false", tc.in)
			}
			if got.Platform != tc.want.Platform || got.ID != tc.want.ID ||
				got.Start != tc.want.Start || got.Page != tc.want.Page {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExtractVideoMedia(t *testing.T) {
	cases := []struct {
		in       string
		wantNil  bool
		wantType string
		wantHost string // substring assertion on URL
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false, "video/youtube", "youtube-nocookie.com"},
		{"https://www.bilibili.com/video/BV1xx411c7mD?p=2", false, "video/bilibili", "player.bilibili.com"},
		{"https://example.com/post/abc", true, "", ""},
		{"", true, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ExtractVideoMedia(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected MediaInfo, got nil")
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if !strings.Contains(got.URL, tc.wantHost) {
				t.Errorf("URL = %q does not contain %q", got.URL, tc.wantHost)
			}
		})
	}
}

func TestRewriteVideoLinks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"bare_youtube",
			"Check out https://www.youtube.com/watch?v=dQw4w9WgXcQ today",
			"Check out [[video:youtube:dQw4w9WgXcQ]] today",
		},
		{
			"markdown_link",
			"See [the video](https://youtu.be/dQw4w9WgXcQ) for more.",
			"See [[video:youtube:dQw4w9WgXcQ]] for more.",
		},
		{
			"bilibili_with_page",
			"https://www.bilibili.com/video/BV1xx411c7mD?p=2",
			"[[video:bilibili:BV1xx411c7mD?page=2]]",
		},
		{
			"non_video_url_untouched",
			"https://example.com/post/abc and a [link](https://example.org/x)",
			"https://example.com/post/abc and a [link](https://example.org/x)",
		},
		{
			"existing_placeholder_idempotent",
			"[[video:youtube:dQw4w9WgXcQ]]",
			"[[video:youtube:dQw4w9WgXcQ]]",
		},
		{
			"two_matches",
			"a https://youtu.be/dQw4w9WgXcQ b https://youtu.be/abcdEFGHijk c",
			"a [[video:youtube:dQw4w9WgXcQ]] b [[video:youtube:abcdEFGHijk]] c",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteVideoLinks(tc.in)
			if got != tc.want {
				t.Errorf("RewriteVideoLinks() =\n  got:  %q\n  want: %q", got, tc.want)
			}
		})
	}
}

func TestRewriteVideoIframes(t *testing.T) {
	cases := []struct {
		name     string
		html     string
		wantHas  string // substring expected in result
		wantMiss string // substring that must not survive
	}{
		{
			"youtube_iframe",
			`<div><p>before</p><iframe src="https://www.youtube.com/embed/dQw4w9WgXcQ"></iframe><p>after</p></div>`,
			"[[video:youtube:dQw4w9WgXcQ]]",
			"<iframe",
		},
		{
			"bilibili_iframe",
			`<iframe src="https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=2"></iframe>`,
			"[[video:bilibili:BV1xx411c7mD?page=2]]",
			"<iframe",
		},
		{
			"unrelated_iframe_left_alone",
			`<iframe src="https://example.com/widget"></iframe>`,
			"<iframe",  // unchanged
			"[[video",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + tc.html + "</body></html>"))
			if err != nil {
				t.Fatal(err)
			}
			RewriteVideoIframes(doc.Selection)
			out, _ := doc.Find("body").Html()
			if !strings.Contains(out, tc.wantHas) {
				t.Errorf("expected %q in %q", tc.wantHas, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Errorf("expected %q to be absent in %q", tc.wantMiss, out)
			}
		})
	}
}
