package ai

import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestBuildInsightPromptCandidatesIncludeIDsAndReadMarker(t *testing.T) {
	topics := []model.InterestTopic{{Topic: "AI", Weight: 5.0}}
	tags := []model.InterestTag{{Tag: "transformers", Weight: 3.0}}
	titles := []string{"Why GPT-5 matters"}
	cands := []model.InsightCandidate{
		{
			Article:    model.Article{ID: 123, Title: "Mixture of Experts deep dive", FeedTitle: "ML Weekly"},
			BriefShort: "How sparse routing works",
		},
		{
			Article:     model.Article{ID: 456, Title: "Old favorite on RAG", FeedTitle: "Search Blog"},
			AlreadyRead: true,
			BriefShort:  "",
		},
	}
	got := BuildInsightPrompt(topics, tags, titles, cands)
	mustContain := []string{
		"[id=123]",
		"Mixture of Experts deep dive",
		"ML Weekly",
		"How sparse routing works",
		"[id=456]",
		"[r]",
		"Old favorite on RAG",
		"\"recommendations\"",
		"json", // schema/format hint somewhere in instructions
		"core",
		"emerging",
		"AI",
		"transformers",
		"Why GPT-5 matters",
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, got)
		}
	}
}

func TestBuildInsightPromptEmptyCandidatesStillProducesPrompt(t *testing.T) {
	got := BuildInsightPrompt(nil, nil, nil, nil)
	if !strings.Contains(got, "\"recommendations\"") {
		t.Errorf("prompt should still describe schema even when empty:\n%s", got)
	}
}
