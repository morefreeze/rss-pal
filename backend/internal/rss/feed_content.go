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
	doc.Find(`script:not([type^="math/tex"]), style, noscript, template`).Remove()
	PromoteLazyImages(doc)
	ResolveURLs(doc, articleURL)
	// Heading permalink glyphs are navigation, not part of heading text.
	doc.Find("a.anchor").Remove()
	return ExtractMarkdown(doc.Find("body"))
}

func feedContainsHTML(raw string) bool {
	candidate := markdownCodeForHTMLDetection.ReplaceAllString(raw, "")
	candidate = strings.ReplaceAll(candidate, "< ", "&lt; ")
	tokenizer := html.NewTokenizer(strings.NewReader(candidate))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			rawToken := string(tokenizer.Raw())
			if len(rawToken) < 2 || rawToken[1] == ' ' {
				continue
			}
			token := tokenizer.Token()
			// Known HTML elements only: Markdown autolinks and literal <Type>
			// placeholders must not turn otherwise plain Markdown into HTML.
			if token.DataAtom != 0 && strings.Contains(feedElementNames, "|"+token.Data+"|") {
				return true
			}
		}
	}
}

// HTML atoms also include attribute names such as type; only element names indicate markup.
const feedElementNames = "|a|abbr|address|article|aside|audio|b|blockquote|body|br|button|caption|cite|code|col|colgroup|data|dd|del|details|dfn|dialog|div|dl|dt|em|embed|fieldset|figcaption|figure|footer|form|h1|h2|h3|h4|h5|h6|head|header|hgroup|hr|html|i|iframe|img|input|ins|kbd|label|legend|li|link|main|mark|menu|meta|nav|noscript|object|ol|option|p|picture|pre|q|rp|rt|ruby|s|samp|script|section|select|small|source|span|strong|style|sub|summary|sup|table|tbody|td|template|textarea|tfoot|th|thead|time|title|tr|track|u|ul|var|video|wbr|"
