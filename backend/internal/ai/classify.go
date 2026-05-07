package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

// extractJSON returns the substring from the first '{' to its matching '}',
// or "" if no balanced object is found. Tolerates markdown fences and prefix text.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func parseClassification(raw string) (*model.Classification, error) {
	j := extractJSON(raw)
	if j == "" {
		return nil, fmt.Errorf("no JSON object in AI response")
	}
	var cls model.Classification
	if err := json.Unmarshal([]byte(j), &cls); err != nil {
		return nil, fmt.Errorf("invalid classification JSON: %w", err)
	}
	cls.Topic = strings.TrimSpace(cls.Topic)
	cleaned := cls.Tags[:0]
	for _, t := range cls.Tags {
		t = strings.TrimSpace(t)
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	cls.Tags = cleaned
	return &cls, nil
}

// ClassifyArticle asks the AI to assign one topic + 3-5 tags to an article.
// recommendedTopics is the B3 vocabulary list (DB-frequency-driven + seeds).
func (s *Summarizer) ClassifyArticle(ctx context.Context, title, content string,
	recommendedTopics []string) (*model.Classification, error) {
	content = truncateContent(content)
	rec := strings.Join(recommendedTopics, ", ")
	prompt := fmt.Sprintf(`你是文章分类助手。请分析以下文章并返回 JSON：

{"topic": "...", "tags": ["...", "...", "..."]}

- topic：单选，最贴合的主题。优先从已有主题中选：[%s]，
  如均不贴合可创建新主题（控制在 2-4 字的中文名词）。
- tags：3-5 个具体关键词（人名、产品名、公司、概念）。

仅输出 JSON，无其他内容。

标题：%s

内容：
%s`, rec, title, content)

	raw, err := s.call(ctx, prompt, 200)
	if err != nil {
		return nil, err
	}
	return parseClassification(raw)
}
