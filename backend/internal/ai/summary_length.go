package ai

import "fmt"

const (
	briefMaxCharacters    = 300
	detailedMaxCharacters = 2000
)

// This is a generation instruction, not a post-generation truncation step.
func appendSummaryLengthInstruction(prompt string, maxCharacters int) string {
	return prompt + fmt.Sprintf("\n\n篇幅要求：本次总结的完整输出总字符数不超过%d（包括标题、标点、空白、Markdown 标记及链接地址；中文、英文字母和数字均各按一个字符计算）。如模板已有更严格的篇幅限制，遵守更严格的限制。先规划篇幅，优先保留核心观点、关键依据和结论，合并或省略次要细节；不要逐段展开，也不要为凑字数扩写。必须在字符上限内完整收尾，写完所有句子、列表项、表格行和 Markdown 标记，不要在半句话或未完成的结构处停止，不要输出截断占位语。", maxCharacters)
}
