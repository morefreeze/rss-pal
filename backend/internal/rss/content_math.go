package rss

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Protect source TeX in text nodes before Markdown escapes backslashes. Code,
// scripts and styles are literal content and are deliberately not scanned.
func extractRawTextMath(selection *goquery.Selection, phs []mathPlaceholder) []mathPlaceholder {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "code", "pre", "script", "style", "textarea":
				return
			}
		}
		if n.Type == html.TextNode {
			s := n.Data
			var out strings.Builder
			for i := 0; i < len(s); {
				open, close, display := "", "", false
				switch {
				case strings.HasPrefix(s[i:], `\(`):
					open, close = `\(`, `\)`
				case strings.HasPrefix(s[i:], `\[`):
					open, close, display = `\[`, `\]`, true
				case strings.HasPrefix(s[i:], "$$"):
					open, close, display = "$$", "$$", true
				case s[i] == '$':
					open, close = "$", "$"
				case s[i] == '\\' && i+1 < len(s):
					out.WriteString(s[i : i+2])
					i += 2
					continue
				}
				if open != "" {
					start := i + len(open)
					end := -1
					for j := start; j < len(s); j++ {
						if strings.HasPrefix(s[j:], close) {
							end = j
							break
						}
						if s[j] == '\\' {
							j++
						}
					}
					if end >= 0 {
						body := s[start:end]
						valid := strings.TrimSpace(body) != ""
						if open == "$" {
							valid = valid && body == strings.TrimSpace(body) && !strings.ContainsAny(body, "\n\r") && !shouldEscapeProseDollarPair([]rune(body))
						}
						if valid {
							key := fmt.Sprintf("⁣RSSPALMATH%d⁣", len(phs))
							phs = append(phs, mathPlaceholder{key: key, latex: body, display: display})
							out.WriteString(key)
							i = end + len(close)
							continue
						}
						// An invalid price pair may end at a real formula's opener.
						// Keep it literal here; escape prices after HTML conversion.
						if open == "$" && shouldEscapeProseDollarPair([]rune(body)) {
							out.WriteByte('$')
							i++
							continue
						}
						out.WriteString(s[i : end+len(close)])
						i = end + len(close)
						continue
					}
				}
				out.WriteByte(s[i])
				i++
			}
			n.Data = out.String()
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range selection.Nodes {
		walk(n)
	}
	return phs
}

// A price's apparent closing dollar can actually open the next math span.
func dollarStartsMath(r []rune) bool {
	for j := 1; j < len(r) && r[j] != '\n'; j++ {
		if r[j] == '\\' {
			j++
			continue
		}
		if r[j] == '$' {
			body := string(r[1:j])
			return body != "" && body == strings.TrimSpace(body) && !shouldEscapeProseDollarPair(r[1:j])
		}
	}
	return false
}

func feedHasMath(raw string) bool {
	if !strings.ContainsAny(raw, "$\\") && !strings.Contains(raw, "math") && !strings.Contains(raw, "katex") {
		return false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	return err == nil && len(extractTexAnnotations(doc.Selection)) > 0
}
