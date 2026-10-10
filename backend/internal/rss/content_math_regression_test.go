package rss

import (
	"github.com/PuerkitoBio/goquery"
	"strings"
	"testing"
)

func TestMathSurvivesContentPipelines(t *testing.T) {
	raw := `<article><p>Boolean ring: $x \\in B$ and $2x=0$.</p><script type="math/tex; mode=display">x=\\frac{a}{b}</script><p>Price $9 and $200. <code>$x \\in B$</code></p><script>evil()</script></article>`
	raw = strings.ReplaceAll(raw, `\\`, `\`)
	for _, pipeline := range []string{"feed", "page"} {
		t.Run(pipeline, func(t *testing.T) {
			got := NormalizeFeedContent(raw, "https://example.org/a")
			if pipeline == "page" {
				var err error
				got, err = NewContentFetcher().FetchContentFromReader(strings.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, want := range []string{`$x \in B$`, `$2x=0$`, `$$`, `x=\frac{a}{b}`, "`$x \\in B$`"} {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
			if strings.Contains(got, "evil()") || strings.Contains(got, `$x \\in B$`) {
				t.Errorf("unsafe or doubled formula: %q", got)
			}
		})
	}
}
func TestMathExtractionDoesNotMutateSelection(t *testing.T) {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(`<article><span class="math">\(x_i\)</span></article>`))
	sel := doc.Find("article")
	first := ExtractMarkdown(sel)
	second := ExtractMarkdown(sel)
	if first != second || strings.Contains(second, "RSSPALMATH") {
		t.Fatalf("first=%q second=%q", first, second)
	}
}

func TestRawMathDelimitersAndCode(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`<p>Then \(x_i\) and \[x=\frac{a}{b}\] done.</p>`, `$x_i$`},
		{`<p>Then $$x=\frac{a}{b}$$ done.</p>`, "$$\n" + `x=\frac{a}{b}` + "\n$$"},
		{`<p>Cost $9 and $200, use $x \in B$.</p>`, `$x \in B$`},
		{`<p><code>$x \in B$</code></p>`, "`$x \\in B$`"},
		{`<p><span class="katex"><annotation encoding="application/x-tex">x_i</annotation></span> $y_i$</p>`, `$x_i$ $y_i$`},
	}
	for _, tt := range cases {
		got := NormalizeFeedContent(tt.raw, "https://example.org")
		if !strings.Contains(got, tt.want) {
			t.Errorf("raw=%s got=%q want=%q", tt.raw, got, tt.want)
		}
	}
}

func TestSinglePriceBeforeFormula(t *testing.T) {
	got := NormalizeFeedContent(`<p>Cost $9; use $x \in B$ or $2x=0$.</p>`, "https://example.org")
	want := `Cost \$9; use $x \in B$ or $2x=0$.`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = NormalizeFeedContent(`<p>Price $9 and $200.</p>`, "https://example.org")
	if got != `Price \$9 and \$200.` {
		t.Fatalf("price escaped twice: %q", got)
	}
}

func TestNumericInlineMath(t *testing.T) {
	got := NormalizeFeedContent(`<p>Modulo $2$, degree $4$, send $x$ to $0$.</p>`, "https://example.org")
	want := `Modulo $2$, degree $4$, send $x$ to $0$.`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSubscriptionFallbackPreservesMath(t *testing.T) {
	raw := `<p>Ring $x \in B$.</p><script type="math/tex; mode=display">2x=0</script>`
	for _, tt := range []struct{ description, content string }{{raw, ""}, {"", raw}, {"Brief summary", raw}} {
		got, _ := BuildItemContent(tt.description, tt.content, "https://example.org/post")
		if !strings.Contains(got, `$x \in B$`) || !strings.Contains(got, "$$\n2x=0\n$$") {
			t.Errorf("lost subscription math: %q", got)
		}
	}
}
