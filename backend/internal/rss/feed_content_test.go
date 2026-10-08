package rss

import (
	"strings"
	"testing"
)

func TestNormalizeFeedContentRichHTML(t *testing.T) {
	raw := `<section class="markdown-section"><h2>Heading</h2><blockquote><ruby>中文<rt>translation</rt></ruby></blockquote><p><a href="../next">Next</a></p><img data-src="image.png" alt="Photo"><pre><code>&lt;div&gt;sample&lt;/div&gt;</code></pre><table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table><script>bad()</script></section>`
	got := NormalizeFeedContent(raw, "https://example.com/posts/one")
	for _, want := range []string{"## Heading", "中文", "translation", "[Next](https://example.com/next)", "![Photo](https://example.com/posts/image.png)", "<div>sample</div>", "| A | B |"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"<section", "<ruby", "<rt", "<img", "bad()"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("retained %q in %q", unwanted, got)
		}
	}
}

func TestNormalizeFeedContentPreservesMarkdownAndPlainText(t *testing.T) {
	for _, raw := range []string{"", "plain  text\nsecond line", "# Title\n\n**bold** and [link](https://example.com)", "# Code\n\n```html\n<div>example</div>\n```\n\nUse `<span>` here.", "a < b && c > d", "Contact <person@example.com> or <https://example.com>"} {
		if got := NormalizeFeedContent(raw, "https://example.com"); got != raw {
			t.Errorf("got %q want %q", got, raw)
		}
	}
}
