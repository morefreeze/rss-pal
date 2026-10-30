package api

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
	pinyin "github.com/mozillazg/go-pinyin"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
)

// Search adds a bounded excerpt, never full article content, to the list DTO.
type ArticleSearchItem struct {
	ArticleListItem
	SearchSnippet string `json:"search_snippet"`
}

func searchPlainText(source string) string {
	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(source), &rendered); err != nil {
		return source
	}
	doc, err := goquery.NewDocumentFromReader(&rendered)
	if err != nil {
		return source
	}
	doc.Find("p,li,pre,blockquote,h1,h2,h3,h4,h5,h6,br,td,th").Each(func(_ int, s *goquery.Selection) {
		for _, node := range s.Nodes {
			node.AppendChild(&html.Node{Type: html.TextNode, Data: " "})
		}
	})
	return strings.Join(strings.Fields(doc.Text()), " ")
}

// Return rune offsets for either literal text or the existing pinyin fallback.
func searchMatchIndex(text, query string, allowPinyin bool) int {
	folded := strings.ToLower(text)
	needle := strings.ToLower(query)
	if needle == "" {
		return -1
	}
	if index := strings.Index(folded, needle); index >= 0 {
		return utf8.RuneCountInString(folded[:index])
	}
	if !allowPinyin {
		return -1
	}
	for _, r := range needle {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return -1
		}
	}
	args := pinyin.NewArgs()
	args.Fallback = func(r rune, _ pinyin.Args) []string {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return []string{strings.ToLower(string(r))}
		}
		return nil
	}
	var full, initials strings.Builder
	var positions, initialPositions []int
	for index, r := range []rune(text) {
		for _, token := range pinyin.LazyPinyin(string(r), args) {
			full.WriteString(token)
			for i := 0; i < len(token); i++ {
				positions = append(positions, index)
			}
			if token != "" {
				initials.WriteByte(token[0])
				initialPositions = append(initialPositions, index)
			}
		}
	}
	if i := strings.Index(full.String(), needle); i >= 0 {
		return positions[i]
	}
	if i := strings.Index(initials.String(), needle); i >= 0 {
		return initialPositions[i]
	}
	return -1
}

func searchExcerpt(text, query string) string {
	runes := []rune(text)
	match := searchMatchIndex(text, query, true)
	start := 0
	if match > 55 {
		start = match - 55
	}
	end := start + 160
	if end > len(runes) {
		end = len(runes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}

func articleToSearchItem(a model.Article, query string) ArticleSearchItem {
	body := searchPlainText(a.Content)
	brief := searchPlainText(a.SummaryBrief)
	candidates := []string{body, brief, a.Title, a.URL, a.FeedTitle, strings.Join(strings.Fields(a.Content), " ")}
	selected := brief
	for _, text := range candidates {
		if searchMatchIndex(text, query, false) >= 0 {
			selected = text
			break
		}
	}
	if searchMatchIndex(selected, query, false) < 0 {
		for _, text := range []string{brief, a.Title, a.FeedTitle} {
			if searchMatchIndex(text, query, true) >= 0 {
				selected = text
				break
			}
		}
	}
	return ArticleSearchItem{ArticleListItem: articleToListItem(a, nil), SearchSnippet: searchExcerpt(selected, query)}
}
