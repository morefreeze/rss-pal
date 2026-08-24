package ai

import (
	"fmt"
	"regexp"
	"strings"
)

const articleAnchorPrefix = "article-section-"

var (
	articleATXHeadingRE = regexp.MustCompile(`^ {0,3}#{1,6}(?:[[:space:]]|$)`)
	articleListItemRE   = regexp.MustCompile(`^[[:space:]]*(?:[*+-]|[0-9]+[.)])[[:space:]]+`)
	articleBlockquoteRE = regexp.MustCompile(`^ {0,3}>[[:space:]]?`)
	articleLinkRE       = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)
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

// articleAnchorID returns the stable, zero-based ID used by summary prompts.
func articleAnchorID(index int) string {
	return fmt.Sprintf("%s%03d", articleAnchorPrefix, index)
}

// annotateArticleForSummary inserts one line-oriented marker before every
// addressable Markdown block. The source text and its line ending style are
// otherwise left untouched.
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

func annotateArticle(content string) (string, int) {
	lines := splitArticleAnchorLines(content)
	if len(lines) == 0 {
		return content, 0
	}

	newline := articleAnchorNewline(lines)
	var out strings.Builder
	out.Grow(len(content) + len(lines)*32)

	count := 0
	var active articleBlockKind
	var fence byte
	fenceLen := 0
	for _, line := range lines {
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
			active = articleBlockNone
			out.WriteString(line.text)
			out.WriteString(line.ending)
			continue
		}

		kind, start := articleAnchorLineKind(line.text, active)
		imageOnly := articleAnchorIsImageOnly(line.text, kind)
		if start && !imageOnly {
			out.WriteString(fmt.Sprintf("[正文锚点: %s]%s", articleAnchorID(count), newline))
			count++
		}
		out.WriteString(line.text)
		out.WriteString(line.ending)
		if imageOnly && kind == articleBlockBlockquote {
			active = articleBlockNone
		} else {
			active = kind
		}
	}

	return out.String(), count
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
