package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
)

func newSummaryPromptCapture(t *testing.T) (*ai.Summarizer, *[]string) {
	t.Helper()
	prompts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		for _, message := range request.Messages {
			if message.Role == "user" {
				prompts = append(prompts, message.Content)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(server.Close)
	return ai.NewSummarizerWithModel("test-key", server.URL, "test-model"), &prompts
}

func TestSummarizerServiceEmptyBodyKeepsTitleWithoutArticleAnchors(t *testing.T) {
	summarizer, prompts := newSummaryPromptCapture(t)
	service := NewSummarizerService(summarizer)

	if _, _, err := service.Summarize(context.Background(), &model.Article{Title: "Title only"}); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(*prompts) != 2 {
		t.Fatalf("captured %d prompts, want 2", len(*prompts))
	}
	for i, prompt := range *prompts {
		if !strings.Contains(prompt, "标题：Title only") {
			t.Errorf("prompt %d does not preserve article title:\n%s", i, prompt)
		}
	}
	detailed := (*prompts)[1]
	if strings.Contains(detailed, "内容：\nTitle only") {
		t.Errorf("detailed prompt substitutes title into empty body:\n%s", detailed)
	}
	if strings.Contains(detailed, "[正文锚点:") || strings.Contains(detailed, "#article-section-NNN") {
		t.Errorf("empty body unexpectedly advertises article anchors:\n%s", detailed)
	}
}

func TestSummarizerServiceNonemptyBodyStillAnnotatesDetailedPrompt(t *testing.T) {
	summarizer, prompts := newSummaryPromptCapture(t)
	service := NewSummarizerService(summarizer)

	article := &model.Article{Title: "Title", Content: "Body paragraph"}
	if _, _, err := service.Summarize(context.Background(), article); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(*prompts) != 2 {
		t.Fatalf("captured %d prompts, want 2", len(*prompts))
	}
	if strings.Contains((*prompts)[0], "[正文锚点:") {
		t.Errorf("brief prompt unexpectedly contains article marker:\n%s", (*prompts)[0])
	}
	detailed := (*prompts)[1]
	if !strings.Contains(detailed, "[正文锚点: article-section-001]\nBody paragraph") ||
		!strings.Contains(detailed, "#article-section-NNN") {
		t.Errorf("nonempty detailed prompt missing article anchors:\n%s", detailed)
	}
}
