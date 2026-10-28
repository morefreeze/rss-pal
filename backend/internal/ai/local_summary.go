package ai

import (
	"fmt"
	"strings"
)

// LocalSummaryPrompts prepares text-only browser calls without credentials or
// provider requests. Keep the same grounding, length and source-anchor policy.
func LocalSummaryPrompts(title, content, briefTemplate, detailedTemplate string) (string, string) {
	content = prepareArticleContent(content)
	if briefTemplate != "" || detailedTemplate != "" {
		brief := strings.NewReplacer("{title}", title, "{content}", content).Replace(briefTemplate)
		return systemGuardrail + "\n\n" + appendSummaryLengthInstruction(brief, briefMaxCharacters), systemGuardrail + "\n\n" + buildDetailedTemplatePrompt(title, content, detailedTemplate)
	}
	brief := fmt.Sprintf("请为以下文章提炼核心要点，每条以 • 开头，只输出要点列表。\n标题：%s\n内容：\n%s", title, content)
	detailedContent, anchors := buildDetailedArticlePromptInput(content)
	detailed := fmt.Sprintf("请为以下文章生成精炼的中文总结，保留重要依据、背景和结论。\n标题：%s\n内容：\n%s", title, detailedContent)
	return systemGuardrail + "\n\n" + appendDefaultSummaryInstruction(brief, content, false, ""), systemGuardrail + "\n\n" + appendDetailedArticleAnchorInstruction(appendDefaultSummaryInstruction(detailed, content, true, ""), anchors)
}
