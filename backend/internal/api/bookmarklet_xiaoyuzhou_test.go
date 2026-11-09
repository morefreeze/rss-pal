package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestBookmarkletXiaoyuzhouAudioOutsideArticle(t *testing.T) {
	const episode = "https://www.xiaoyuzhoufm.com/episode/6af0b82eb4a4e20202f6bda7"
	const audio = "https://media.xyzcdn.net/podcast/episode-recording.m4a"
	for _, tc := range []struct {
		name, page, media string
		wantAudio         bool
	}{
		{"episode with sharing parameters", episode + "?s=share", audio, true},
		{"podcast profile", "https://www.xiaoyuzhoufm.com/podcast/123", audio, false},
		{"other site", "https://example.org/episode/123", audio, false},
		{"non-audio metadata", episode, "https://media.xyzcdn.net/podcast/episode-video.mp4", false},
		{"embedded credentials", episode, "https://user@media.xyzcdn.net/podcast/episode-recording.m4a", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := fmt.Sprintf(`<html><head><meta property="og:audio" content="%s"></head>
				<body><audio src="%s"></audio><main><article><p>%s</p></article></main></body></html>`,
				tc.media, audio, strings.Repeat("Episode show notes. ", 25))
			content, media, err := extractArticleFromHTML(html, tc.page)
			if err != nil || !strings.Contains(content, "Episode show notes.") {
				t.Fatalf("content=%q err=%v", content, err)
			}
			if tc.wantAudio {
				if media == nil || media.URL != audio || media.Type != "audio/mp4" {
					t.Fatalf("media=%+v, want episode audio", media)
				}
			} else if media != nil {
				t.Fatalf("unexpected media=%+v", media)
			}
		})
	}
}
