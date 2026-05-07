package ai

import (
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

// BuildInsightPrompt builds the user prompt for JSON-mode insight generation.
// Candidates are rendered as "[id=N] Title — feed · brief" so the AI references
// exact ids in the response. "[r]" prefixes already-read candidates so the AI
// can frame them as "revisit" recommendations when relevant.
func BuildInsightPrompt(topics []model.InterestTopic, tags []model.InterestTag,
	recentTitles []string, candidates []model.InsightCandidate) string {

	var b strings.Builder
	b.WriteString(`基于用户的兴趣画像和最近阅读，请输出严格 json：

{
  "markdown": "（4 段中文 markdown：核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）",
  "recommendations": [
    {
      "direction": "...",
      "direction_kind": "core" | "emerging",
      "articles": [
        {"article_id": <候选池中的整数 id>, "reason": "≤100 字"}
      ]
    }
  ]
}

约束：
- direction 共 2-3 个；总文章数 3-5 篇
- core = 强化已有核心兴趣；emerging = 弱信号反复出现的新兴趣点
- article_id 必须严格来自下方候选池，禁止编造 / 改动 id
- 每个 reason ≤ 100 字，必须解释为什么属于该方向
- 候选池为空或无合适文章时，"recommendations": []
- 输出必须是合法 JSON，不要包裹 markdown 代码块

`)

	if len(topics) > 0 {
		b.WriteString("## 用户兴趣主题（按权重）\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "- %s (%.2f)\n", t.Topic, t.Weight)
		}
		b.WriteString("\n")
	}

	if len(tags) > 0 {
		b.WriteString("## 用户关键词（top 20，按权重）\n")
		max := 20
		if len(tags) < max {
			max = len(tags)
		}
		for i := 0; i < max; i++ {
			fmt.Fprintf(&b, "- %s (%.2f)\n", tags[i].Tag, tags[i].Weight)
		}
		b.WriteString("\n")
	}

	if len(recentTitles) > 0 {
		b.WriteString("## 最近阅读\n")
		for _, t := range recentTitles {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## 候选文章池（共 %d 条；[r] 表示已读过的过往收藏）\n", len(candidates))
	for _, c := range candidates {
		marker := ""
		if c.AlreadyRead {
			marker = "[r] "
		}
		feed := c.Article.FeedTitle
		brief := c.BriefShort
		switch {
		case feed != "" && brief != "":
			fmt.Fprintf(&b, "[id=%d] %s%s — %s · %s\n", c.Article.ID, marker, c.Article.Title, feed, brief)
		case feed != "":
			fmt.Fprintf(&b, "[id=%d] %s%s — %s\n", c.Article.ID, marker, c.Article.Title, feed)
		default:
			fmt.Fprintf(&b, "[id=%d] %s%s\n", c.Article.ID, marker, c.Article.Title)
		}
	}

	return b.String()
}
