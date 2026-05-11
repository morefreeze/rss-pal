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
	// Category must be a known enum value — out-of-enum values are coerced
	// to "" so the worker writes NULL rather than polluting the schema.
	cls.Category = strings.TrimSpace(cls.Category)
	if !model.IsValidCategory(cls.Category) {
		cls.Category = ""
	}
	return &cls, nil
}

// ClassifyArticle asks the AI to assign one topic + 3-5 tags to an article.
// recommendedTopics is the B3 vocabulary list (DB-frequency-driven + seeds).
func (s *Summarizer) ClassifyArticle(ctx context.Context, title, content string,
	recommendedTopics []string) (*model.Classification, error) {
	content = truncateContent(content)
	rec := strings.Join(recommendedTopics, ", ")
	cats := strings.Join(model.ValidCategories, ", ")
	prompt := fmt.Sprintf(`你是文章分类助手。请分析以下文章并返回 JSON：

{"topic": "...", "tags": ["...", "...", "..."], "category": "..."}

- topic：单选，最贴合的细粒度主题。优先从已有主题中选：[%s]，
  如均不贴合可创建新主题（控制在 2-4 字的中文名词）。
- tags：3-5 个具体关键词（人名、产品名、公司、概念）。
- category：从以下闭合列表里**必选一个**，不允许新建：[%s]。
  含义：ai_eng=AI 工程实践 / ai=AI 通用资讯 / cn_tech=中文科技动态 /
        enterprise=企业基建与开发工具 / youtube=视频 / podcast=播客 /
        news=时事新闻 / blog=博客随笔评论 / health=健康 / business=商业财经。

仅输出 JSON，无其他内容。

标题：%s

内容：
%s`, rec, cats, title, content)

	// Use callJSON (response_format=json_object) so GLM-4.5 emits a parseable
	// object instead of spending tokens on chain-of-thought prose. 1500
	// max_tokens leaves comfortable headroom for reasoning + the short JSON
	// payload; the previous 200-token budget was being entirely consumed by
	// reasoning, leaving Content empty.
	raw, err := s.callJSON(ctx, prompt, 1500)
	if err != nil {
		return nil, err
	}
	return parseClassification(raw)
}
