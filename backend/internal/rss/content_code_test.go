package rss

import (
	"github.com/PuerkitoBio/goquery"
	"strings"
	"testing"
)

func TestHighlightedCodePreservesLines(t *testing.T) {
	raw := `<article><p>Example</p><figure class="highlight js"><figcaption>sample.js <a href="https://example.com/source">Source</a></figcaption><table><tr><td class="gutter"><pre>1<br>2<br>3<br></pre></td><td class="code"><pre><span class="line">if (ready) {</span><br><span class="line">  run(&quot;$value&quot;);</span><br><span class="line">}</span><br></pre></td></tr></table></figure><table><tr><th>A<br>B</th></tr><tr><td>C</td></tr></table></article>`
	for _, mode := range []string{"page", "feed", "subscription"} {
		t.Run(mode, func(t *testing.T) {
			var got string
			if mode == "subscription" {
				got, _ = BuildItemContent("summary", raw, "https://example.com")
			} else if mode == "feed" {
				got = NormalizeFeedContent(raw, "https://example.com")
			} else {
				got, _ = NewContentFetcher().FetchContentFromReader(strings.NewReader(raw))
			}
			if !strings.Contains(got, "```js\nif (ready) {\n  run(\"$value\");\n}\n```") {
				t.Fatalf("damaged code:\n%s", got)
			}
			if !strings.Contains(got, "sample.js") || !strings.Contains(got, "[Source](https://example.com/source)") {
				t.Fatal("caption lost")
			}
			if strings.Contains(got, "1 2 3") || strings.Contains(got, "```\n1") {
				t.Fatal("gutter retained")
			}
			if !strings.Contains(got, "A B") {
				t.Fatal("table regression")
			}
		})
	}
}
func TestPlainPrePreservesBreaksAndWhitespace(t *testing.T) {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(`<pre><code class="language-python">if True:<br>    print(&quot;Privacy Policy&quot;)<br><br><br>    print(&quot;  two spaces&quot;)</code></pre>`))
	got := cleanContent(ExtractMarkdown(doc.Find("body")))
	if !strings.Contains(got, "if True:\n    print(\"Privacy Policy\")\n\n\n    print(\"  two spaces\")") {
		t.Fatalf("damaged code: %s", got)
	}
}
func TestCleanContentPreservesFencedCode(t *testing.T) {
	for _, fence := range []string{"```", "~~~~"} {
		code := fence + "text\n  Privacy Policy\n\n\n    keep  spaces\n" + fence
		got := cleanContent("Outside  spaces\n\n\n" + code + "\n\nFollow us on X")
		if got != "Outside spaces\n\n"+code {
			t.Fatalf("fence altered: %q", got)
		}
	}
}
