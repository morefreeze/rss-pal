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

func TestNumericSymbolicMathDoesNotCorruptFollowingFormula(t *testing.T) {
	for _, text := range []string{
		`Training takes $6N$ and $C \approx 6ND$ operations.`,
		`Compare $2ND$, $5x+3$ and $C \approx 6ND$.`,
	} {
		for _, pipeline := range []string{"feed", "page"} {
			t.Run(pipeline+"/"+text, func(t *testing.T) {
				raw := "<article><p>" + text + "</p></article>"
				got := NormalizeFeedContent(raw, "https://example.org/article")
				if pipeline == "page" {
					var err error
					got, err = NewContentFetcher().FetchContentFromReader(strings.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
				}
				if got != text {
					t.Errorf("formula conversion got %q, want %q", got, text)
				}
			})
		}
	}
}

func TestRejectedPricePairDoesNotConsumeFollowingFormula(t *testing.T) {
	got := NormalizeFeedContent(`<p>Price $9 dollars$ and $C \approx 6ND$.</p>`, "https://example.org")
	want := `Price \$9 dollars\$ and $C \approx 6ND$.`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// These source expressions include delimiter-adjacent spaces accepted by
// MathJax. They must bypass Markdown escaping just like unpadded formulas.
// https://lilianweng.github.io/posts/2017-06-21-overview/ (Autoencoders)
// https://lilianweng.github.io/posts/2017-09-28-information-bottleneck/ (Markov chain)
func TestPaddedRawMathPreservesSourceTeX(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`The approximation function $ f(x) \approx x $ reproduces the input.`, `The approximation function $f(x) \approx x$ reproduces the input.`},
		{`One state of a Markov Chain: $ h_i \to h_{i+1}$. According to DPI.`, `One state of a Markov Chain: $h_i \to h_{i+1}$. According to DPI.`},
		{`Compare $2ND $, $ 6N$, $ C \approx 6ND $ and price $9 and $200.`, `Compare $2ND$, $6N$, $C \approx 6ND$ and price \$9 and \$200.`},
		{`Cost $9; use $ x \in B $ or $ 2x=0 $.`, `Cost \$9; use $x \in B$ or $2x=0$.`},
	} {
		for _, pipeline := range []string{"feed", "page"} {
			t.Run(pipeline+"/"+tc.source, func(t *testing.T) {
				raw := "<article><p>" + tc.source + "</p></article>"
				got := NormalizeFeedContent(raw, "https://example.org/post")
				if pipeline == "page" {
					var err error
					got, err = NewContentFetcher().FetchContentFromReader(strings.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
				}
				if got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			})
		}
	}
}

func TestPaddedMathPreservesMatrixRowSeparators(t *testing.T) {
	tex := `\begin{pmatrix}a_i & b \\ c & d\end{pmatrix}`
	raw := `<article><p>Matrix $ ` + tex + ` $ and <code>$ x_i \to y $</code>.</p>`
	got := NormalizeFeedContent(raw, "https://example.org")
	if !strings.Contains(got, "$"+tex+"$") || !strings.Contains(got, "`$ x_i \\to y $`") {
		t.Fatalf("source math or literal code changed: %q", got)
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
