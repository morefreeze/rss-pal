package rss

import (
	"html"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// PlainExcerpt strips publisher markup while retaining literal code and comparisons.
func PlainExcerpt(raw string) string {
	if !feedContainsHTML(raw) {
		raw = html.UnescapeString(raw)
		if !feedContainsHTML(raw) {
			return strings.Join(strings.Fields(raw), " ")
		}
	}
	// Protect code spans from the HTML parser, which otherwise drops literal tags.
	codes := []string{}
	prefix := "RSSPALCODE"
	for strings.Contains(raw, prefix) {
		prefix += "X"
	}
	raw = markdownCodeForHTMLDetection.ReplaceAllStringFunc(raw, func(s string) string {
		codes = append(codes, s)
		return prefix + strconv.Itoa(len(codes)) + "END"
	})
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return ""
	}
	doc.Find("script,style,noscript,template").Remove()
	doc.Find("p,div,section,article,br,li,h1,h2,h3,h4,pre,table,tr,td").Each(func(_ int, s *goquery.Selection) { s.BeforeHtml(" "); s.AfterHtml(" ") })
	result := doc.Find("body").Text()
	for i, s := range codes {
		result = strings.ReplaceAll(result, prefix+strconv.Itoa(i+1)+"END", s)
	}
	return strings.Join(strings.Fields(result), " ")
}
