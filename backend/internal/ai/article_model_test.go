package ai

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bytedance/rss-pal/internal/airouting"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArticleModelBoundary(t *testing.T) {
	for _, n := range []int{9999, 10000, 10001} {
		t.Run(string(rune(n)), func(t *testing.T) {
			want := "glm-5.3-flash"
			if n > 10000 {
				want = "glm-5.3"
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q chatRequest
				json.NewDecoder(r.Body).Decode(&q)
				calls++
				if q.Model != want {
					t.Errorf("n=%d model=%s want=%s", n, q.Model, want)
				}
				if n > 10000 && !strings.Contains(q.Messages[1].Content.(string), "正文末尾") {
					t.Error("long article truncated at old limit")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"choices":[{"message":{"content":"summary"}}]}`))
			}))
			defer srv.Close()
			s := NewSummarizerWithModel("test", srv.URL, "glm-5.3-flash")
			s.SetAdmission(nil)
			_, err := s.Summarize(context.Background(), "title", strings.Repeat("文", n-4)+"正文末尾")
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || s.Model() != "glm-5.3-flash" {
				t.Fatal("shared client mutated", calls)
			}
		})
	}
}

func TestArticleSelectionPreservesFallbackAndCustomModels(t *testing.T) {
	s := NewSummarizerWithModel("", "", "glm-5.3-flash")
	long := s.forArticle(context.Background(), strings.Repeat("文", 10001))
	if long.forArticle(context.Background(), "short fallback").Model() != "glm-5.3" {
		t.Fatal("fallback changed selected model")
	}
	if len([]rune(long.truncateArticle(strings.Repeat("文", 100001)))) < 100000 {
		t.Fatal("long cap too small")
	}
	custom := NewSummarizerWithModel("", "", "custom-model")
	if custom.forArticle(context.Background(), strings.Repeat("文", 10001)).Model() != "custom-model" {
		t.Fatal("custom model overwritten")
	}
}
func TestLongArticleStreamingAndTemplateRouting(t *testing.T) {
	for _, stream := range []bool{false, true} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var q chatRequest
			json.NewDecoder(r.Body).Decode(&q)
			calls++
			if q.Model != "glm-5.3" {
				t.Error(q.Model)
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n"))
			} else {
				w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
			}
		}))
		s := NewSummarizerWithModel("test", srv.URL, "glm-5.3-flash")
		s.SetAdmission(nil)
		body := strings.Repeat("文", 10001)
		var err error
		if stream {
			_, err = s.SummarizeWithTemplateStream(context.Background(), "title", body, "{content}", "{content}", func(string) {}, func(string) {})
		} else {
			_, err = s.SummarizeWithTemplate(context.Background(), "title", body, "{content}", "{content}")
		}
		srv.Close()
		if err != nil || calls != 2 {
			t.Fatal(err, calls)
		}
	}
}

func TestConfiguredRoutesPinCompanyAndOriginalLength(t *testing.T) {
	s := NewSummarizerWithModel("old-key", "https://old.invalid", "custom-model")
	calls := 0
	s.SetArticleResolver(func(ctx context.Context, n int) (airouting.Target, error) {
		calls++
		if n <= 10000 {
			return airouting.Target{Model: "grok-small", APIKey: "small-key", BaseURL: "https://small.invalid", Protocol: "openai"}, nil
		}
		return airouting.Target{Model: "claude-large", APIKey: "large-key", BaseURL: "https://large.invalid", Protocol: "anthropic", Large: true}, nil
	})
	a := s.forArticle(context.Background(), strings.Repeat("文", 10000))
	b := s.forArticle(context.Background(), strings.Repeat("文", 10001))
	if a.model != "grok-small" || b.model != "claude-large" || a.apiKey != "small-key" || b.providerProtocol != "anthropic" {
		t.Fatal("wrong company route")
	}
	if b.forArticle(context.Background(), "short fallback") != b || calls != 2 {
		t.Fatal("route changed mid-operation")
	}
	if s.model != "custom-model" || s.apiKey != "old-key" {
		t.Fatal("mutated shared summarizer")
	}
	if len([]rune(a.truncateArticle(strings.Repeat("文", 10000)))) != 10000 {
		t.Fatal("small article truncated before 10000")
	}
}
func TestConfiguredRouteFailureDoesNotCallProvider(t *testing.T) {
	s := NewSummarizerWithModel("key", "https://invalid.invalid", "glm-5.3-flash")
	s.SetArticleResolver(func(context.Context, int) (airouting.Target, error) {
		return airouting.Target{}, errors.New("settings unavailable")
	})
	if _, e := s.Summarize(context.Background(), "title", "body"); e == nil || !strings.Contains(e.Error(), "settings unavailable") {
		t.Fatal(e)
	}
}

func TestArticleRoutePreservesOnlySameEndpointVision(t *testing.T) {
	s := NewSummarizerWithModel("key", "https://original.invalid", "glm-5.3-flash")
	s.SetVisionModel("vision-original")
	s.SetArticleResolver(func(context.Context, int) (airouting.Target, error) {
		return airouting.Target{APIKey: "key", BaseURL: "https://original.invalid", Model: "glm-5.3"}, nil
	})
	if s.forArticle(context.Background(), "body").visionModel != "vision-original" {
		t.Fatal("legacy vision changed")
	}
	s.SetArticleResolver(func(context.Context, int) (airouting.Target, error) {
		return airouting.Target{APIKey: "new-key", BaseURL: "https://other.invalid", Model: "claude-sonnet"}, nil
	})
	if s.forArticle(context.Background(), "body").visionModel != "claude-sonnet" {
		t.Fatal("vision model crossed companies")
	}
}
