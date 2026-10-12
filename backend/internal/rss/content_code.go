package rss

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Hexo highlights use a layout table with a separate line-number column.
// Convert that wrapper before ordinary data-table normalization runs.
func normalizeCodeBlocks(selection *goquery.Selection) {
	selection.Find("figure.highlight").Each(func(_ int, figure *goquery.Selection) {
		code := figure.Find("td.code pre")
		if code.Length() != 1 {
			return
		}
		language := ""
		for _, class := range strings.Fields(figure.AttrOr("class", "")) {
			if class != "highlight" && codeLanguage.MatchString(class) {
				language = class
				break
			}
		}
		pre := &html.Node{Type: html.ElementNode, Data: "pre"}
		node := &html.Node{Type: html.ElementNode, Data: "code"}
		if language != "" {
			node.Attr = []html.Attribute{{Key: "class", Val: "language-" + language}}
		}
		for child := code.Get(0).FirstChild; child != nil; {
			next := child.NextSibling
			code.Get(0).RemoveChild(child)
			node.AppendChild(child)
			child = next
		}
		pre.AppendChild(node)
		code.Closest("table").ReplaceWithNodes(pre)
	})
	selection.Find("pre br").Each(func(_ int, br *goquery.Selection) { br.ReplaceWithNodes(&html.Node{Type: html.TextNode, Data: "\n"}) })
}

var codeLanguage = regexp.MustCompile(`^[A-Za-z0-9_+-]+$`)
var codeFenceStart = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// Protect fenced code from prose whitespace and boilerplate cleanup, including
// arbitrary-length fences and an unterminated final block.
func protectCodeFences(content string) (string, func(string) string) {
	prefix := "RSSPALCODEBLOCK"
	for strings.Contains(content, prefix) {
		prefix += "X"
	}
	lines := strings.SplitAfter(content, "\n")
	var out strings.Builder
	var blocks []string
	for i := 0; i < len(lines); i++ {
		opening := codeFenceStart.FindStringSubmatch(strings.TrimSuffix(lines[i], "\n"))
		if opening == nil || (opening[1][0] == '`' && strings.Contains(opening[2], "`")) {
			out.WriteString(lines[i])
			continue
		}
		start := i
		for i++; i < len(lines); i++ {
			closing := strings.TrimSuffix(lines[i], "\n")
			leading := len(closing) - len(strings.TrimLeft(closing, " "))
			closing = strings.TrimSpace(closing)
			if leading <= 3 && len(closing) >= len(opening[1]) && strings.Trim(closing, string(opening[1][0])) == "" {
				break
			}
		}
		end := i + 1
		if end > len(lines) {
			end = len(lines)
		}
		block := strings.Join(lines[start:end], "")
		token := fmt.Sprintf("%s%dEND", prefix, len(blocks))
		blocks = append(blocks, strings.TrimSuffix(block, "\n"))
		out.WriteString(token)
		out.WriteByte('\n')
	}
	return out.String(), func(text string) string {
		for i, block := range blocks {
			text = strings.ReplaceAll(text, fmt.Sprintf("%s%dEND", prefix, i), block)
		}
		return text
	}
}

func feedHasCodeBlock(raw string) bool {
	if !strings.Contains(strings.ToLower(raw), "<pre") {
		return false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	return err == nil && doc.Find("pre").Length() > 0
}
