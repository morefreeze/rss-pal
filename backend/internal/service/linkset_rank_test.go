package service

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/rss"
)

func TestPrerankCandidates_TopicMatchBoosts(t *testing.T) {
	cands := []rss.Candidate{
		{Title: "Go generics deep dive", URL: "https://example.com/go", EditorNote: ""},
		{Title: "Cooking with cast iron", URL: "https://example.com/iron", EditorNote: ""},
	}
	topics := []model.InterestTopic{{Topic: "Go", Weight: 1.0}}
	hosts := &model.HostSignalSet{}
	scores := PrerankCandidates(cands, topics, hosts)

	if scores[0] <= scores[1] {
		t.Fatalf("topic-matching candidate should outrank: got %v, %v", scores[0], scores[1])
	}
	if scores[0] < 0.35 {
		t.Errorf("topic match should add at least 0.35; got %v", scores[0])
	}
}

func TestPrerankCandidates_LikedHostBoosts(t *testing.T) {
	cands := []rss.Candidate{
		{Title: "anything", URL: "https://liked.example/a"},
		{Title: "anything", URL: "https://random.example/a"},
	}
	hosts := &model.HostSignalSet{
		Liked: map[string]struct{}{"liked.example": {}},
	}
	scores := PrerankCandidates(cands, nil, hosts)
	if scores[0] <= scores[1] {
		t.Fatalf("liked-host candidate should outrank")
	}
}

func TestPrerankCandidates_DislikedHostPenalised(t *testing.T) {
	cands := []rss.Candidate{
		{Title: "x", URL: "https://bad.example/a"},
		{Title: "x", URL: "https://ok.example/a"},
	}
	hosts := &model.HostSignalSet{
		Disliked: map[string]struct{}{"bad.example": {}},
	}
	scores := PrerankCandidates(cands, nil, hosts)
	if scores[0] >= scores[1] {
		t.Fatalf("disliked candidate should rank lower; got %v, %v", scores[0], scores[1])
	}
}

func TestPrerankCandidates_BaselineFloor(t *testing.T) {
	cands := []rss.Candidate{{Title: "x", URL: "https://x.example/a"}}
	scores := PrerankCandidates(cands, nil, &model.HostSignalSet{})
	if scores[0] < 0.05 || scores[0] > 0.1 {
		t.Errorf("baseline should be ≈0.05, got %v", scores[0])
	}
}

func TestPrerankCandidates_BoundedToOne(t *testing.T) {
	cands := []rss.Candidate{
		{Title: "go go go", URL: "https://liked.example/a", EditorNote: "go go go"},
	}
	topics := []model.InterestTopic{{Topic: "go", Weight: 1.0}}
	hosts := &model.HostSignalSet{
		Liked:     map[string]struct{}{"liked.example": {}},
		Completed: map[string]struct{}{"liked.example": {}},
	}
	scores := PrerankCandidates(cands, topics, hosts)
	if scores[0] > 1.0 {
		t.Errorf("score should be capped at 1.0; got %v", scores[0])
	}
}
