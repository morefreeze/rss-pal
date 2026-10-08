package rss

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// FeedContentVersion identifies content normalized for the Markdown reader.
// Store it alongside cached content; never infer the format of an already
// converted body from code examples or inline angle brackets.
const FeedContentVersion = 1

var markdownCodeForHTMLDetection = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~|``[^`]*``|`[^`]*`")

// NormalizeFeedContent accepts a feed's decoded HTML, plain text or Markdown.
// Unlike full-page extraction, the complete feed fragment is already the body:
// do not select only its first article/section or remove newsletter containers.
func NormalizeFeedContent(raw, articleURL string) string {
	if !feedContainsHTML(raw) {
		return raw
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	doc.Find("script, style, noscript, template").Remove()
	PromoteLazyImages(doc)
	ResolveURLs(doc, articleURL)
	// Heading permalink glyphs are navigation, not part of heading text.
	doc.Find("a.anchor").Remove()
	return ExtractMarkdown(doc.Find("body"))
}

func feedContainsHTML(raw string) bool {
	candidate := markdownCodeForHTMLDetection.ReplaceAllString(raw, "")
	tokenizer := html.NewTokenizer(strings.NewReader(candidate))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			// Known HTML elements only: Markdown autolinks and literal <Type>
			// placeholders must not turn otherwise plain Markdown into HTML.
			if token.DataAtom != 0 {
				return true
			}
		}
	}
}
