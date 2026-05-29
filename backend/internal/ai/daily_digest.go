package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// DailyCandidate is the minimum payload BuildDailyPrompt needs.
type DailyCandidate struct {
	Idx          int
	Title        string
	SummaryBrief string
}

// BuildDailyPrompt produces the Chinese prompt asking the model to pick
// nPick of the given candidates and write an 80-120 字 intro.
func BuildDailyPrompt(candidates []DailyCandidate, nPick int) string {
	var b strings.Builder
	for _, c := range candidates {
		fmt.Fprintf(&b, "[%d] 《%s》\n    摘要：%s\n\n", c.Idx, c.Title, truncateContent(c.SummaryBrief))
	}
	return fmt.Sprintf(`以下是过去 24 小时按个性化推荐分数挑出的 %d 篇候选文章：

%s请从中精选 %d 篇组成「今日精选日报」,并写一段 80-120 字的中文导语,回答这个问题:
「为什么这 %d 篇值得今天读?这些文章共同指向什么趋势或思考?」

要求:
- 严格输出 JSON,不要 Markdown 代码块,不要任何包裹文字:
  {"picks":[i,j,k,l,m],"intro":"..."}
- picks 是 %d 个互不相同的 0-%d 整数下标,按推荐顺序排列。
- intro 80-120 字(中文字符数),从候选中提炼共同主题、张力或对比;不要逐篇复述;不要 Markdown、不要分点列表;语气专业、克制。`,
		len(candidates), b.String(), nPick, nPick, nPick, len(candidates)-1)
}

// ParseDailyDigestJSON parses the model output and validates picks + intro.
// nCandidates is the upper bound for pick indices (exclusive).
// nPick is the exact pick count expected.
func ParseDailyDigestJSON(raw string, nCandidates, nPick int) (picks []int, intro string, err error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var payload struct {
		Picks []int  `json:"picks"`
		Intro string `json:"intro"`
	}
	if jerr := json.Unmarshal([]byte(raw), &payload); jerr != nil {
		return nil, "", fmt.Errorf("daily digest parse: %w", jerr)
	}

	if len(payload.Picks) != nPick {
		return nil, "", fmt.Errorf("daily digest: picks len %d, want %d", len(payload.Picks), nPick)
	}
	seen := make(map[int]bool, nPick)
	for _, p := range payload.Picks {
		if p < 0 || p >= nCandidates {
			return nil, "", fmt.Errorf("daily digest: pick %d out of [0,%d)", p, nCandidates)
		}
		if seen[p] {
			return nil, "", fmt.Errorf("daily digest: duplicate pick %d", p)
		}
		seen[p] = true
	}

	intro = strings.TrimSpace(payload.Intro)
	runes := utf8.RuneCountInString(intro)
	if runes < 60 || runes > 250 {
		return nil, "", fmt.Errorf("daily digest: intro %d runes, want 60-250", runes)
	}

	return payload.Picks, intro, nil
}

// GenerateDailyDigest asks the model to pick min(5, len(candidates)) and write
// the intro. Returns (nil, "", nil) when candidates is empty.
func (s *Summarizer) GenerateDailyDigest(ctx context.Context, candidates []DailyCandidate) (picks []int, intro string, err error) {
	if len(candidates) == 0 {
		return nil, "", nil
	}
	nPick := 5
	if len(candidates) < nPick {
		nPick = len(candidates)
	}
	prompt := BuildDailyPrompt(candidates, nPick)
	raw, callErr := s.call(ctx, prompt, 800)
	if callErr != nil {
		return nil, "", callErr
	}
	return ParseDailyDigestJSON(raw, len(candidates), nPick)
}
