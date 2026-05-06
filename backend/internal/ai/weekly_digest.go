package ai

import (
	"context"
	"fmt"
	"strings"
)

// GenerateWeeklyIntro produces a Chinese 150-200 word intro that frames the
// theme of the week given the titles + brief summaries of the top articles.
// Returns "" + nil when articles is empty.
func (s *Summarizer) GenerateWeeklyIntro(ctx context.Context, articles []WeeklyDigestItem) (string, error) {
	if len(articles) == 0 {
		return "", nil
	}

	var b strings.Builder
	for i, a := range articles {
		fmt.Fprintf(&b, "%d. 《%s》\n   摘要：%s\n\n", i+1, a.Title, truncateContent(a.SummaryBrief))
	}

	prompt := `以下是本周精选的若干篇文章的标题和摘要：

` + b.String() + `请用 150-200 字的中文写一段「本周主题导语」，回答这个问题：
「为什么这一周值得读者关注？这些文章共同指向什么趋势或思考？」

要求：
- 不要逐篇复述；要从中提炼出共同主题、张力或对比。
- 给读者一个清晰的「为什么这周值得关注」视角。
- 语气专业、克制，避免营销化措辞。
- 直接输出导语正文，不要标题、不要 Markdown、不要分点列表。`

	return s.call(ctx, prompt, 600)
}

// WeeklyDigestItem is the minimum payload the prompt needs.
type WeeklyDigestItem struct {
	Title        string
	SummaryBrief string
}
