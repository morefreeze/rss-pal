package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

const (
	maxDirections    = 3
	maxArticlesTotal = 5
	maxReasonRunes   = 200
)

type insightEnvelope struct {
	Markdown        string                          `json:"markdown"`
	Recommendations []model.RecommendationDirection `json:"recommendations"`
}

// ParseInsightJSON extracts markdown + validated recommendations from raw AI
// output. The candidate-id whitelist is enforced; entries failing any rule are
// dropped (with a reason recorded) instead of failing the whole insight.
//
// On total parse failure it returns the raw string as markdown so the user
// still sees something readable; recs are nil; drop reasons explain why.
func ParseInsightJSON(raw string, candidateIDs map[int]bool) (string, []model.RecommendationDirection, []string) {
	dropped := []string{}

	body := stripCodeFence(strings.TrimSpace(raw))
	if !strings.HasPrefix(body, "{") {
		if j := extractJSON(body); j != "" {
			body = j
		}
	}

	var env insightEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		dropped = append(dropped, fmt.Sprintf("json parse failed: %v", err))
		return raw, nil, dropped
	}

	out := make([]model.RecommendationDirection, 0, len(env.Recommendations))
	totalArticles := 0
	for di, d := range env.Recommendations {
		if len(out) >= maxDirections {
			dropped = append(dropped, fmt.Sprintf("direction[%d] over cap %d", di, maxDirections))
			continue
		}
		if d.Direction == "" {
			dropped = append(dropped, fmt.Sprintf("direction[%d] empty name", di))
			continue
		}
		if d.DirectionKind != "core" && d.DirectionKind != "emerging" {
			dropped = append(dropped, fmt.Sprintf("direction[%d] bad kind %q", di, d.DirectionKind))
			continue
		}
		validArticles := make([]model.ArticleRecommendation, 0, len(d.Articles))
		for ai_, a := range d.Articles {
			if totalArticles >= maxArticlesTotal {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] over cap %d", di, ai_, maxArticlesTotal))
				continue
			}
			if !candidateIDs[a.ArticleID] {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] id=%d not in pool", di, ai_, a.ArticleID))
				continue
			}
			reason := strings.TrimSpace(a.Reason)
			if reason == "" {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] empty reason", di, ai_))
				continue
			}
			if len([]rune(reason)) > maxReasonRunes {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] reason too long", di, ai_))
				continue
			}
			validArticles = append(validArticles, model.ArticleRecommendation{
				ArticleID: a.ArticleID,
				Reason:    reason,
			})
			totalArticles++
		}
		if len(validArticles) == 0 {
			dropped = append(dropped, fmt.Sprintf("direction[%d] no surviving articles", di))
			continue
		}
		out = append(out, model.RecommendationDirection{
			Direction:     d.Direction,
			DirectionKind: d.DirectionKind,
			Articles:      validArticles,
		})
	}

	return env.Markdown, out, dropped
}

// stripCodeFence removes a single leading/trailing ```...``` fence if present.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
