package ai

import (
	"fmt"
	"regexp"
	"strings"
)

const articleAnchorPrefix = "article-section-"

// detailedArticleAnchorInstruction is appended only to detailed-summary
// prompts when the supplied article includes addressable content blocks.
const detailedArticleAnchorInstruction = `请按原文顺序总结，并按语义分组（按大意合并相邻内容）。当文章包含多个清晰的章节或主题组时，添加至少 3 个、至多 30 个 [查看原文](#article-section-NNN) 链接；每个总结组或段落至多一个。NNN 只能使用正文中提供的锚点（来自正文中已有的锚点），并按原文顺序分布链接。不要为了凑够最少数量而给每个正文段落添加链接，不要每段都添加跳转。短文或单一连续主题的文章可以不添加链接；短文或整篇只讲一件事时可以完全不添加；除上述例外，不得只添加 1 或 2 个链接。示例：[查看原文](#article-section-003)。`

var (
	articleATXHeadingRE            = regexp.MustCompile(`^ {0,3}#{1,6}(?:[[:space:]]|$)`)
	articleListItemRE              = regexp.MustCompile(`^[[:space:]]*(?:[*+-]|[0-9]+[.)])[[:space:]]+`)
	articleBlockquoteRE            = regexp.MustCompile(`^ {0,3}>[[:space:]]?`)
	articleLinkRE                  = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)
	articleImageAltWithBlankLineRE = regexp.MustCompile(`!\[([^\]]*\n[ \t]*\n[^\]]*)\]\(([^)\s]+)\)`)
	articleBlankLineRunRE          = regexp.MustCompile(`[ \t]*\n([ \t]*\n)+[ \t]*`)
)

type articleAnchorLine struct {
	text   string
	ending string
}

type articleBlockKind uint8

const (
	articleBlockNone articleBlockKind = iota
	articleBlockParagraph
	articleBlockHeading
	articleBlockBlockquote
	articleBlockList
)

// articleAnchorID returns the stable, one-based ID used by summary prompts.
func articleAnchorID(index int) string {
	return fmt.Sprintf("%s%03d", articleAnchorPrefix, index)
}

// annotateArticleForSummary inserts one line-oriented marker before every
// addressable Markdown block. Its transient prompt input is first normalized
// to match frontend rendering; stored Markdown is never changed here.
func annotateArticleForSummary(content string) string {
	annotated, _ := annotateArticle(content)
	return annotated
}

// hasAddressableArticleBlock reports whether content contains at least one
// block that can be referred to by an article summary.
func hasAddressableArticleBlock(content string) bool {
	_, count := annotateArticle(content)
	return count > 0
}

// buildDetailedArticlePromptInput prepares already-truncated content for a
// detailed-summary prompt. It leaves content without addressable blocks alone
// so image-only and empty articles do not advertise unusable anchors.
func buildDetailedArticlePromptInput(content string) (annotated, instruction string) {
	annotated, count := annotateArticle(content)
	if count == 0 {
		return content, ""
	}
	return annotated, detailedArticleAnchorInstruction
}

func appendDetailedArticleAnchorInstruction(prompt, instruction string) string {
	if instruction == "" {
		return prompt
	}
	return prompt + "\n\n" + instruction
}

func annotateArticle(content string) (string, int) {
	content = normalizeArticleAnchorSource(content)
	lines := splitArticleAnchorLines(content)
	if len(lines) == 0 {
		return content, 0
	}

	newline := articleAnchorNewline(lines)
	var out strings.Builder
	out.Grow(len(content) + len(lines)*32)

	nextID := 1
	count := 0
	var active articleBlockKind
	var fence byte
	fenceLen := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line.text)
		if fence != 0 {
			out.WriteString(line.text)
			out.WriteString(line.ending)
			if articleAnchorIsFence(line.text, fence, fenceLen) {
				fence = 0
				fenceLen = 0
			}
			continue
		}

		if char, length, ok := articleAnchorFenceStart(line.text); ok {
			active = articleBlockNone
			fence = char
			fenceLen = length
			out.WriteString(line.text)
			out.WriteString(line.ending)
			continue
		}

		if trimmed == "" || articleAnchorIsThematicBreak(line.text) {
			if active != articleBlockList || !articleAnchorListHasContinuation(lines, i) {
				active = articleBlockNone
			}
			out.WriteString(line.text)
			out.WriteString(line.ending)
			continue
		}

		if articleAnchorIsTopLevelIndentedCode(line.text) &&
			!(active == articleBlockList && articleListItemRE.MatchString(line.text)) {
			active = articleBlockNone
			out.WriteString(line.text)
			out.WriteString(line.ending)
			continue
		}

		kind, start := articleAnchorLineKind(line.text, active)
		imageOnly := articleAnchorIsImageOnly(line.text, kind)
		if imageOnly && kind == articleBlockParagraph && active != articleBlockParagraph && articleAnchorParagraphHasMeaningfulContinuation(lines, i) {
			imageOnly = false
		}
		if start && !imageOnly {
			out.WriteString(fmt.Sprintf("[正文锚点: %s]%s", articleAnchorID(nextID), newline))
			nextID++
			count++
		}
		out.WriteString(line.text)
		out.WriteString(line.ending)
		if imageOnly && kind == articleBlockBlockquote && active != articleBlockBlockquote {
			active = articleBlockNone
		} else {
			active = kind
		}
	}

	return out.String(), count
}

// normalizeArticleAnchorSource mirrors the frontend anchor normalization.
// Blank lines inside image alt text are destructive to CommonMark block
// boundaries, so both prompt annotation and rendering scan this same form.
func normalizeArticleAnchorSource(content string) string {
	return articleImageAltWithBlankLineRE.ReplaceAllStringFunc(content, func(match string) string {
		sub := articleImageAltWithBlankLineRE.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		alt := strings.TrimSpace(articleBlankLineRunRE.ReplaceAllString(sub[1], " "))
		return "![" + alt + "](" + sub[2] + ")"
	})
}

func articleAnchorParagraphHasMeaningfulContinuation(lines []articleAnchorLine, start int) bool {
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line.text) == "" || articleAnchorIsThematicBreak(line.text) {
			return false
		}
		if _, _, ok := articleAnchorFenceStart(line.text); ok || articleAnchorIsTopLevelIndentedCode(line.text) {
			return false
		}
		kind, _ := articleAnchorLineKind(line.text, articleBlockParagraph)
		if kind != articleBlockParagraph {
			return false
		}
		if !articleAnchorIsImageOnly(line.text, articleBlockParagraph) {
			return true
		}
	}
	return false
}

func articleAnchorListHasContinuation(lines []articleAnchorLine, blankLine int) bool {
	for i := blankLine + 1; i < len(lines); i++ {
		text := lines[i].text
		if strings.TrimSpace(text) == "" {
			continue
		}
		return strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t")
	}
	return false
}

func articleAnchorIsTopLevelIndentedCode(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

func splitArticleAnchorLines(content string) []articleAnchorLine {
	if content == "" {
		return nil
	}
	lines := make([]articleAnchorLine, 0, strings.Count(content, "\n")+1)
	for start := 0; start < len(content); {
		end := strings.IndexByte(content[start:], '\n')
		if end < 0 {
			lines = append(lines, articleAnchorLine{text: content[start:]})
			break
		}
		end += start
		text := content[start:end]
		ending := "\n"
		if strings.HasSuffix(text, "\r") {
			text = strings.TrimSuffix(text, "\r")
			ending = "\r\n"
		}
		lines = append(lines, articleAnchorLine{text: text, ending: ending})
		start = end + 1
	}
	return lines
}

func articleAnchorNewline(lines []articleAnchorLine) string {
	for _, line := range lines {
		if line.ending != "" {
			return line.ending
		}
	}
	return "\n"
}

func articleAnchorFenceStart(line string) (byte, int, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	char := trimmed[0]
	length := 0
	for length < len(trimmed) && trimmed[length] == char {
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	return char, length, true
}

func articleAnchorIsFence(line string, char byte, minimumLength int) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(trimmed) < minimumLength || trimmed[0] != char {
		return false
	}
	length := 0
	for length < len(trimmed) && trimmed[length] == char {
		length++
	}
	return length >= minimumLength && strings.TrimSpace(trimmed[length:]) == ""
}

func articleAnchorLineKind(line string, active articleBlockKind) (articleBlockKind, bool) {
	switch {
	case articleATXHeadingRE.MatchString(line):
		return articleBlockHeading, true
	case articleListItemRE.MatchString(line):
		return articleBlockList, true
	case articleBlockquoteRE.MatchString(line):
		return articleBlockBlockquote, active != articleBlockBlockquote
	case active == articleBlockList && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
		return articleBlockList, false
	default:
		return articleBlockParagraph, active != articleBlockParagraph
	}
}

func articleAnchorIsThematicBreak(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	char := trimmed[0]
	if char != '-' && char != '*' && char != '_' {
		return false
	}
	count := 0
	for _, r := range trimmed {
		if r == rune(char) {
			count++
			continue
		}
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return count >= 3
}

func articleAnchorIsImageOnly(line string, kind articleBlockKind) bool {
	text := line
	switch kind {
	case articleBlockHeading:
		text = strings.TrimSpace(text)
		text = strings.TrimSpace(strings.TrimLeft(text, "#"))
	case articleBlockList:
		text = articleListItemRE.ReplaceAllString(text, "")
	case articleBlockBlockquote:
		text = articleBlockquoteRE.ReplaceAllString(text, "")
	}
	text = imgRE.ReplaceAllString(text, "")
	text = articleLinkRE.ReplaceAllString(text, "$1")
	return strings.TrimSpace(text) == ""
}
