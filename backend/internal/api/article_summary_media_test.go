package api

import (
	"context"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/transcript"
)

type stubTranscriptFetcher struct {
	result *transcript.Result
	err    error
	calls  int
}

func (f *stubTranscriptFetcher) Fetch(ctx context.Context, article *model.Article) (*transcript.Result, error) {
	f.calls++
	return f.result, f.err
}

func TestUserAIConfigSummarizerCarriesVisionModel(t *testing.T) {
	cfg := &config.Config{
		Claude: config.ClaudeConfig{BaseURL: "https://global.example/v1"},
		AI:     config.AIConfig{Vision: config.VisionConfig{Model: "vision-model"}},
	}
	userCfg := &model.UserAIConfig{
		APIKey:  "user-key",
		BaseURL: "https://user.example/v1",
		Model:   "text-model",
	}

	svc := newUserSummarizerService(userCfg, cfg)

	if svc == nil {
		t.Fatal("expected a summarizer service")
	}
	if got := svc.Summarizer().VisionModel(); got != "vision-model" {
		t.Fatalf("vision model = %q, want %q", got, "vision-model")
	}
}

func TestFetchTranscriptContentForSummaryAppendsVideoTranscript(t *testing.T) {
	fetcher := &stubTranscriptFetcher{result: &transcript.Result{
		Text:   "第一句字幕。\n第二句字幕。",
		Source: "YouTube CC",
	}}
	article := &model.Article{
		Title:     "Video",
		Content:   "原始简介",
		MediaType: "video/youtube",
		MediaURL:  "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
	}

	content, ok, err := fetchTranscriptContentForSummary(context.Background(), fetcher, article)

	if err != nil {
		t.Fatalf("fetchTranscriptContentForSummary returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected transcript to be found")
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetcher calls = %d, want 1", fetcher.calls)
	}
	for _, want := range []string{"原始简介", "## 字幕", "> 来源：YouTube CC", "第一句字幕。", "第二句字幕。"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q:\n%s", want, content)
		}
	}
}

func TestFetchTranscriptContentForSummarySkipsNonVideo(t *testing.T) {
	fetcher := &stubTranscriptFetcher{result: &transcript.Result{Text: "ignored", Source: "YouTube CC"}}
	article := &model.Article{Content: "正文", MediaType: "text/html"}

	content, ok, err := fetchTranscriptContentForSummary(context.Background(), fetcher, article)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("non-video article should not report transcript content")
	}
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if fetcher.calls != 0 {
		t.Fatalf("fetcher should not be called for non-video articles")
	}
}

func TestLacksSummarizableMediaContent(t *testing.T) {
	tests := []struct {
		name    string
		article *model.Article
		want    bool
	}{
		{
			name:    "empty video needs transcript or content",
			article: &model.Article{MediaType: "video/bilibili"},
			want:    true,
		},
		{
			name:    "video with transcript content can summarize",
			article: &model.Article{MediaType: "video/youtube", Content: "## 字幕\n\nhello"},
			want:    false,
		},
		{
			name:    "audio with page content can summarize",
			article: &model.Article{MediaType: "audio/mpeg", Content: "podcast show notes"},
			want:    false,
		},
		{
			name:    "empty text article is not handled by media guard",
			article: &model.Article{MediaType: "text/html"},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lacksSummarizableMediaContent(tt.article); got != tt.want {
				t.Fatalf("lacksSummarizableMediaContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
