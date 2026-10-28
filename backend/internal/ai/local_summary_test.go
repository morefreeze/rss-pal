package ai

import (
	"fmt"
	"strings"
	"testing"
)

func TestLocalSummaryPromptsPreserveLongArticleConditions(t *testing.T) {
	const condition = "若用户尚未给出背景才询问；共享提示词变更必须运行完整评测。"
	for _, n := range []int{8001, 10000, 10001, 27000} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/custom=%t", n, custom), func(t *testing.T) {
				content := strings.Repeat("文", n) + "\n\n" + condition
				briefTemplate, detailedTemplate := "", ""
				if custom {
					briefTemplate, detailedTemplate = "BRIEF {content}", "DETAIL {content}"
				}
				brief, detailed := LocalSummaryPrompts("title", content, briefTemplate, detailedTemplate)
				for i, prompt := range []string{brief, detailed} {
					if !strings.Contains(prompt, condition) {
						t.Errorf("prompt %d silently lost the article's tail condition", i)
					}
					if !strings.Contains(prompt, "条件与对应结论一起呈现") {
						t.Errorf("prompt %d lacks the condition-preservation contract", i)
					}
				}
			})
		}
	}
}

func TestLocalSummaryPromptsKeepBoundedArticleInput(t *testing.T) {
	content := strings.Repeat("文", 100000) + "不应送出的尾部"
	brief, detailed := LocalSummaryPrompts("title", content, "{content}", "{content}")
	for _, prompt := range []string{brief, detailed} {
		if strings.Contains(prompt, "不应送出的尾部") || !strings.Contains(prompt, "内容已截断") || strings.Count(prompt, "文") < 100000 {
			t.Fatal("local article context must use the same explicit bounded input as server summaries")
		}
	}
}
