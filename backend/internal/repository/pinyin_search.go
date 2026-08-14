package repository

import (
	"strings"
	"unicode"

	"github.com/bytedance/rss-pal/internal/model"
	pinyin "github.com/mozillazg/go-pinyin"
)

func normalizePinyinToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pinyinContains(text, query string) bool {
	needle := normalizePinyinToken(query)
	if needle == "" || strings.TrimSpace(text) == "" {
		return false
	}
	args := pinyin.NewArgs()
	args.Style = pinyin.Normal
	args.Heteronym = false
	args.Fallback = func(r rune, _ pinyin.Args) []string {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return []string{strings.ToLower(string(r))}
		}
		return nil
	}
	parts := pinyin.LazyPinyin(text, args)
	var full strings.Builder
	var initials strings.Builder
	for _, part := range parts {
		token := normalizePinyinToken(part)
		if token == "" {
			continue
		}
		full.WriteString(token)
		initials.WriteByte(token[0])
	}
	return strings.Contains(full.String(), needle) || strings.Contains(initials.String(), needle)
}

func articleMatchesPinyinSearch(article model.Article, query string) bool {
	return pinyinContains(article.Title, query) ||
		pinyinContains(article.SummaryBrief, query) ||
		pinyinContains(article.FeedTitle, query)
}
