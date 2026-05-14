package rss

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestFetchContentFromReader_HeadingsAndParagraphs(t *testing.T) {
	html := `<html><body><article>
		<h2>Hello World</h2>
		<p>This is the first paragraph with enough length to clear filters.</p>
		<p>And here is a second paragraph, also long enough to be kept.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "## Hello World") {
		t.Errorf("expected '## Hello World' in output, got:\n%s", got)
	}
	if !strings.Contains(got, "first paragraph") {
		t.Errorf("expected first paragraph text in output, got:\n%s", got)
	}
	if !strings.Contains(got, "second paragraph") {
		t.Errorf("expected second paragraph text in output, got:\n%s", got)
	}
	if strings.Contains(got, "<p>") || strings.Contains(got, "<h2>") {
		t.Errorf("expected raw HTML tags to be stripped from markdown, got:\n%s", got)
	}
	if !strings.Contains(got, "\n\n") {
		t.Errorf("expected paragraph separators (\\n\\n), got:\n%s", got)
	}
}

func TestFetchContentFromReader_PreservesImage(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph long enough to keep around.</p>
		<p><img src="https://example.com/cat.png" alt="a cat"></p>
		<p>Trailing paragraph long enough to keep around.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "![a cat](https://example.com/cat.png)") {
		t.Errorf("expected markdown image, got:\n%s", got)
	}
}

func TestFetchContentFromReader_PreservesCodeBlock(t *testing.T) {
	html := `<html><body><article>
		<p>Here is some Go code we definitely want to keep:</p>
		<pre><code class="language-go">package main

func main() {
	println("hello")
}</code></pre>
		<p>And some text after the code block to pad it out.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "```") {
		t.Errorf("expected fenced code block, got:\n%s", got)
	}
	if !strings.Contains(got, `println("hello")`) {
		t.Errorf("expected code body preserved, got:\n%s", got)
	}
}

func TestFetchContentFromReader_PreservesList(t *testing.T) {
	html := `<html><body><article>
		<p>Reasons we care, padded so the article selector keeps it:</p>
		<ul>
			<li>First reason here</li>
			<li>Second reason here</li>
			<li>Third reason here</li>
		</ul>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	for _, want := range []string{"- First reason", "- Second reason", "- Third reason"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
}

func TestFetchContentFromReader_PreservesTable(t *testing.T) {
	html := `<html><body><article>
		<p>Performance table for the readers in our experiment:</p>
		<table>
			<thead><tr><th>Lib</th><th>QPS</th></tr></thead>
			<tbody>
				<tr><td>A</td><td>120</td></tr>
				<tr><td>B</td><td>340</td></tr>
			</tbody>
		</table>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "| Lib | QPS |") && !strings.Contains(got, "| Lib  | QPS |") {
		t.Errorf("expected GFM table header in output, got:\n%s", got)
	}
	if !strings.Contains(got, "120") || !strings.Contains(got, "340") {
		t.Errorf("expected row values in table output, got:\n%s", got)
	}
}

func TestStripAvatars(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph.</p>
		<p><img class="avatar" src="https://example.com/me.png" alt="me"></p>
		<p><img src="https://www.gravatar.com/avatar/abc"></p>
		<p><img width="32" height="32" src="https://example.com/tiny.png"></p>
		<p><img src="https://example.com/screenshot.png" alt="a real screenshot"></p>
		<p>Trailing paragraph.</p>
	</article></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	StripAvatars(doc)

	imgs := doc.Find("img")
	if imgs.Length() != 1 {
		t.Fatalf("expected 1 surviving img, got %d", imgs.Length())
	}
	src, _ := imgs.First().Attr("src")
	if src != "https://example.com/screenshot.png" {
		t.Errorf("wrong img survived: src=%q", src)
	}
}

func TestFetchContentFromReader_StripsAvatars(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph long enough to keep around for the selector.</p>
		<p><img class="avatar" src="https://example.com/byline.png" alt="me"></p>
		<p><img src="https://example.com/figure.png" alt="figure"></p>
		<p>Trailing paragraph long enough to keep around as well.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if strings.Contains(got, "byline.png") {
		t.Errorf("expected avatar to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "figure.png") {
		t.Errorf("expected real figure to survive, got:\n%s", got)
	}
}

func TestPromoteLazyImages(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		wantSrc string
	}{
		{
			name:    "data-src promoted when src missing",
			html:    `<img data-src="https://example.com/a.png">`,
			wantSrc: "https://example.com/a.png",
		},
		{
			name:    "data-src promoted when src empty",
			html:    `<img src="" data-src="https://example.com/b.png">`,
			wantSrc: "https://example.com/b.png",
		},
		{
			name:    "data-src promoted when src is data: placeholder",
			html:    `<img src="data:image/svg+xml;base64,abc" data-src="https://example.com/c.png">`,
			wantSrc: "https://example.com/c.png",
		},
		{
			name:    "real src wins, data-src ignored",
			html:    `<img src="https://example.com/real.png" data-src="https://example.com/lazy.png">`,
			wantSrc: "https://example.com/real.png",
		},
		{
			name:    "data-original used when data-src absent",
			html:    `<img data-original="https://example.com/orig.png">`,
			wantSrc: "https://example.com/orig.png",
		},
		{
			name:    "data-actual-src (WeChat) used when others absent",
			html:    `<img data-actual-src="https://mmbiz.qpic.cn/x.png">`,
			wantSrc: "https://mmbiz.qpic.cn/x.png",
		},
		{
			name:    "first non-empty wins (data-src before data-original)",
			html:    `<img data-src="https://example.com/first.png" data-original="https://example.com/second.png">`,
			wantSrc: "https://example.com/first.png",
		},
		{
			name:    "no lazy attrs leaves img untouched",
			html:    `<img src="">`,
			wantSrc: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + tc.html + "</body></html>"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			PromoteLazyImages(doc)
			got := doc.Find("img").AttrOr("src", "")
			if got != tc.wantSrc {
				t.Errorf("PromoteLazyImages src = %q, want %q", got, tc.wantSrc)
			}
		})
	}
}

func TestPromoteLazyImages_Srcset(t *testing.T) {
	html := `<img data-src="https://example.com/x.png" data-srcset="https://example.com/x.png 1x, https://example.com/x@2x.png 2x">`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + html + "</body></html>"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	PromoteLazyImages(doc)
	img := doc.Find("img").First()
	if got := img.AttrOr("srcset", ""); got != "https://example.com/x.png 1x, https://example.com/x@2x.png 2x" {
		t.Errorf("expected data-srcset promoted, got %q", got)
	}
}

func TestResolveURLs(t *testing.T) {
	cases := []struct {
		name     string
		baseURL  string
		html     string
		wantImg  string
		wantHref string
	}{
		{
			name:     "site-relative img resolved (the article-562 case)",
			baseURL:  "https://lawsofsoftwareengineering.com",
			html:     `<a href="/book/"><img src="/images/front-cover.png"></a>`,
			wantImg:  "https://lawsofsoftwareengineering.com/images/front-cover.png",
			wantHref: "https://lawsofsoftwareengineering.com/book/",
		},
		{
			name:     "protocol-relative img resolved",
			baseURL:  "https://example.com/post/1",
			html:     `<a href="//other.com/page"><img src="//cdn.example.com/x.png"></a>`,
			wantImg:  "https://cdn.example.com/x.png",
			wantHref: "https://other.com/page",
		},
		{
			name:     "absolute img untouched",
			baseURL:  "https://example.com/post/1",
			html:     `<a href="https://example.com/abs"><img src="https://cdn.example.com/abs.png"></a>`,
			wantImg:  "https://cdn.example.com/abs.png",
			wantHref: "https://example.com/abs",
		},
		{
			name:     "data uri preserved",
			baseURL:  "https://example.com/post/1",
			html:     `<a href="/x"><img src="data:image/png;base64,abc"></a>`,
			wantImg:  "data:image/png;base64,abc",
			wantHref: "https://example.com/x",
		},
		{
			name:     "path-relative img resolved",
			baseURL:  "https://example.com/post/1",
			html:     `<a href="next"><img src="cat.jpg"></a>`,
			wantImg:  "https://example.com/post/cat.jpg",
			wantHref: "https://example.com/post/next",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + tc.html + "</body></html>"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			ResolveURLs(doc, tc.baseURL)
			if got := doc.Find("img").AttrOr("src", ""); got != tc.wantImg {
				t.Errorf("img src = %q, want %q", got, tc.wantImg)
			}
			if got := doc.Find("a").AttrOr("href", ""); got != tc.wantHref {
				t.Errorf("a href = %q, want %q", got, tc.wantHref)
			}
		})
	}
}

func TestResolveURLs_NoopOnBadBase(t *testing.T) {
	html := `<img src="/foo.png"><a href="/bar">x</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + html + "</body></html>"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ResolveURLs(doc, "not-a-url")
	if got := doc.Find("img").AttrOr("src", ""); got != "/foo.png" {
		t.Errorf("bad base should leave src untouched, got %q", got)
	}
	if got := doc.Find("a").AttrOr("href", ""); got != "/bar" {
		t.Errorf("bad base should leave href untouched, got %q", got)
	}
}

func TestFetchContentFromReader_PromotesLazyImage(t *testing.T) {
	// Mimics WeChat: img has data-src but no real src.
	html := `<html><body><article>
		<p>Intro paragraph long enough to keep around for the selector.</p>
		<p><img class="rich_pages wxw-img" data-src="https://mmbiz.qpic.cn/foo.png" data-type="png"></p>
		<p>Trailing paragraph long enough to keep around as well.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "mmbiz.qpic.cn/foo.png") {
		t.Errorf("expected lazy-loaded img URL preserved, got:\n%s", got)
	}
}

func TestIsAvatarImg(t *testing.T) {
	cases := []struct {
		name string
		html string
		want bool
	}{
		{"class avatar", `<img class="avatar" src="https://example.com/x.png">`, true},
		{"class user-avatar", `<img class="user-avatar size-32" src="https://example.com/x.png">`, true},
		{"id author-photo", `<img id="author-photo" src="https://example.com/x.png">`, true},
		{"alt profile picture", `<img alt="John's profile picture" src="https://example.com/x.png">`, true},
		{"alt headshot", `<img alt="Headshot" src="https://example.com/x.png">`, true},
		{"gravatar host", `<img src="https://www.gravatar.com/avatar/abc123">`, true},
		{"avatars path", `<img src="https://cdn.example.com/avatars/u123.png">`, true},
		{"avatar path", `<img src="https://cdn.example.com/avatar/u123.png">`, true},
		{"both dims small", `<img width="32" height="32" src="https://example.com/x.png">`, true},
		{"both dims at threshold", `<img width="64" height="64" src="https://example.com/x.png">`, true},
		{"only width small", `<img width="32" src="https://example.com/x.png">`, false},
		{"only height small", `<img height="32" src="https://example.com/x.png">`, false},
		{"large dims", `<img width="800" height="600" src="https://example.com/x.png">`, false},
		{"plain article image", `<img src="https://example.com/screenshot.png">`, false},
		{"unrelated class", `<img class="wp-image-123" src="https://example.com/x.png">`, false},
		{"non-numeric width", `<img width="auto" height="32" src="https://example.com/x.png">`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			img := doc.Find("img").First()
			got := isAvatarImg(img)
			if got != tc.want {
				t.Errorf("isAvatarImg(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestStripJinaMathShadow(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no math",
			in:   "Just plain text with no math.",
			want: "Just plain text with no math.",
		},
		{
			name: "price not math",
			in:   "a $5 burger costs $5",
			want: "a $5 burger costs $5",
		},
		{
			name: "copyright not signal",
			in:   "see $X = 7$ ©2024 Acme",
			want: "see $X = 7$ ©2024 Acme",
		},
		{
			name: "shadow with unicode minus",
			in:   "consider $x - 1$x−1 must also satisfy",
			want: "consider $x - 1$ must also satisfy",
		},
		{
			name: "shadow with zero-width space",
			in:   "so $\\sqrt{3 + 7} = \\sqrt{10}$3+7​=10​,3-1=2\nnext line",
			want: "so $\\sqrt{3 + 7} = \\sqrt{10}$\nnext line",
		},
		{
			name: "shadow before end of line",
			in:   "and $\\sqrt{10} \\neq 2$10​=2\nmore",
			want: "and $\\sqrt{10} \\neq 2$\nmore",
		},
		{
			name: "pure ascii shadow kept",
			in:   "we get $x = 3$x=3 is the answer",
			want: "we get $x = 3$x=3 is the answer",
		},
		{
			name: "fraction shadow",
			in:   "result $x = \\frac{3 \\pm \\sqrt{33}}{2}$x=2 3±33​​\nend",
			want: "result $x = \\frac{3 \\pm \\sqrt{33}}{2}$\nend",
		},
		{
			name: "no closing dollar",
			in:   "stray $5 dollar bill",
			want: "stray $5 dollar bill",
		},
		{
			name: "newline inside dollars",
			in:   "$x \nstuff$ shadow",
			want: "$x \nstuff$ shadow",
		},
		{
			name: "multiple math on one line",
			in:   "$x \\geq 1$x≥1, valid for $x = 3$x=3.",
			want: "$x \\geq 1$, valid for $x = 3$x=3.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripJinaMathShadow(tc.in)
			if got != tc.want {
				t.Errorf("stripJinaMathShadow(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractTexAnnotations_KatexInline(t *testing.T) {
	html := `<div><p>Since <span class="katex"><span class="katex-mathml"><math><semantics><annotation encoding="application/x-tex">\sqrt{x+7} \ge 0</annotation></semantics></math></span><span class="katex-html" aria-hidden="true">x+7​≥0</span></span> the right side.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `Since $\sqrt{x+7} \ge 0$ the right side.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExtractTexAnnotations_KatexDisplay(t *testing.T) {
	html := `<div><p>Result:</p><span class="katex-display"><span class="katex"><span class="katex-mathml"><math display="block"><semantics><annotation encoding="application/x-tex">x = \frac{3 \pm \sqrt{33}}{2}</annotation></semantics></math></span><span class="katex-html" aria-hidden="true">x=2 3±33​​</span></span></span></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	if !strings.Contains(got, `$$x = \frac{3 \pm \sqrt{33}}{2}$$`) {
		t.Errorf("expected display math block, got:\n%s", got)
	}
	if strings.Contains(got, "x=2 3±33") {
		t.Errorf("shadow should be removed, got:\n%s", got)
	}
}

func TestExtractTexAnnotations_MathJaxV3(t *testing.T) {
	html := `<div><p>Inline <mjx-container class="MathJax" jax="CHTML"><mjx-assistive-mml display="inline"><math><semantics><annotation encoding="application/x-tex">a^2 + b^2 = c^2</annotation></semantics></math></mjx-assistive-mml><mjx-math>a²+b²=c²</mjx-math></mjx-container> here.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `Inline $a^2 + b^2 = c^2$ here.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExtractTexAnnotations_MathJaxV2WithScript(t *testing.T) {
	html := `<div><p>See <span class="MathJax_Preview"></span><span class="MathJax" id="MathJax-Element-1-Frame">x≥1</span><script type="math/tex" id="MathJax-Element-1">x \ge 1</script> always.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `See $x \ge 1$ always.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExtractTexAnnotations_NoMathPassthrough(t *testing.T) {
	html := `<div><p>Just normal prose with no math at all.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `Just normal prose with no math at all.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}

func TestEscapeAmbiguousMathDollars(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no dollars",
			in:   "Just plain prose with no symbols.",
			want: "Just plain prose with no symbols.",
		},
		{
			name: "two prices on one line are escaped",
			in:   "第一次让它裸跑——20 分钟，花了 $9，游戏核心功能根本跑不起来。第二次给它配上完整的 harness——6 小时，花了 $200，游戏可以正常游玩。",
			want: "第一次让它裸跑——20 分钟，花了 \\$9，游戏核心功能根本跑不起来。第二次给它配上完整的 harness——6 小时，花了 \\$200，游戏可以正常游玩。",
		},
		{
			name: "real LaTeX math left intact",
			in:   "result $\\sqrt{x+7} \\ge 0$ end",
			want: "result $\\sqrt{x+7} \\ge 0$ end",
		},
		{
			name: "math with braces left intact",
			in:   "see $a_{i+1}$ here",
			want: "see $a_{i+1}$ here",
		},
		{
			name: "single unpaired dollar untouched",
			in:   "earn $9 a day",
			want: "earn $9 a day",
		},
		{
			name: "letter-led body left intact",
			in:   "we know $x = 1$ holds",
			want: "we know $x = 1$ holds",
		},
		{
			name: "already escaped dollars untouched",
			in:   "earn \\$9 plus \\$200",
			want: "earn \\$9 plus \\$200",
		},
		{
			name: "newline between dollars not paired",
			in:   "$9\n$200",
			want: "$9\n$200",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeAmbiguousMathDollars(tc.in)
			if got != tc.want {
				t.Errorf("escapeAmbiguousMathDollars\n  in:   %q\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFlattenImageAltBlankLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no images",
			in:   "Just plain text.\n\nMore text.",
			want: "Just plain text.\n\nMore text.",
		},
		{
			name: "single-line alt unchanged",
			in:   "![alt](https://example.com/x.png)",
			want: "![alt](https://example.com/x.png)",
		},
		{
			name: "single newline in alt unchanged",
			in:   "![first\nsecond](https://example.com/x.png)",
			want: "![first\nsecond](https://example.com/x.png)",
		},
		{
			name: "trailing blank line collapsed",
			in:   "![Image 1: caption text.\n\n](https://ichef.bbci.co.uk/x.jpg.webp)",
			want: "![Image 1: caption text.](https://ichef.bbci.co.uk/x.jpg.webp)",
		},
		{
			name: "blank line in middle collapsed",
			in:   "![first line\n\nsecond line](https://example.com/x.png)",
			want: "![first line second line](https://example.com/x.png)",
		},
		{
			name: "multiple blank lines collapsed",
			in:   "![alt\n\n\n\n](https://example.com/x.png)",
			want: "![alt](https://example.com/x.png)",
		},
		{
			name: "indented blank line collapsed",
			in:   "![alt\n  \n  ](https://example.com/x.png)",
			want: "![alt](https://example.com/x.png)",
		},
		{
			name: "image inside paragraph context",
			in:   "Lead in.\n\n![cap\n\n](https://example.com/x.png)\n\nFollow up.",
			want: "Lead in.\n\n![cap](https://example.com/x.png)\n\nFollow up.",
		},
		{
			name: "two broken images on a page",
			in:   "![a\n\n](https://e.com/1.png)\n\n![b\n\n](https://e.com/2.png)",
			want: "![a](https://e.com/1.png)\n\n![b](https://e.com/2.png)",
		},
		{
			name: "good image after broken one untouched",
			in:   "![a\n\n](https://e.com/1.png)\n\n![ok](https://e.com/2.png)",
			want: "![a](https://e.com/1.png)\n\n![ok](https://e.com/2.png)",
		},
		{
			name: "idempotent on already-fixed input",
			in:   "![Image 1: caption text.](https://ichef.bbci.co.uk/x.jpg.webp)",
			want: "![Image 1: caption text.](https://ichef.bbci.co.uk/x.jpg.webp)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenImageAltBlankLines(tc.in)
			if got != tc.want {
				t.Errorf("flattenImageAltBlankLines(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAbsolutizeURLs(t *testing.T) {
	html := `<div>
		<img alt="root" src="/images/flowers.png">
		<img alt="rel" src="figures/plot.png">
		<img alt="abs" src="https://cdn.example.com/cat.png">
		<img alt="proto" src="//cdn.example.com/dog.png">
		<a href="/about">about</a>
		<a href="https://other.com/x">other</a>
	</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	AbsolutizeURLs(doc.Selection, "https://elonlit.com/scrivings/a-theory-of-deep-learning/")

	gotSrc := func(alt string) string {
		s, _ := doc.Find(`img[alt="` + alt + `"]`).First().Attr("src")
		return s
	}
	gotHref := func(text string) string {
		h, _ := doc.Find("a").FilterFunction(func(_ int, sel *goquery.Selection) bool {
			return strings.TrimSpace(sel.Text()) == text
		}).First().Attr("href")
		return h
	}
	cases := []struct {
		name, got, want string
	}{
		{"root-relative img", gotSrc("root"), "https://elonlit.com/images/flowers.png"},
		{"path-relative img", gotSrc("rel"), "https://elonlit.com/scrivings/a-theory-of-deep-learning/figures/plot.png"},
		{"absolute img unchanged", gotSrc("abs"), "https://cdn.example.com/cat.png"},
		{"protocol-relative img", gotSrc("proto"), "https://cdn.example.com/dog.png"},
		{"root-relative anchor", gotHref("about"), "https://elonlit.com/about"},
		{"absolute anchor unchanged", gotHref("other"), "https://other.com/x"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s\n  got:  %q\n  want: %q", c.name, c.got, c.want)
		}
	}
}

// Source page (e.g. elonlit.com) defers MathJax rendering — math sits in the
// HTML as raw text inside class="math" containers. Without the raw-text path,
// html-to-markdown escapes every backslash and breaks the LaTeX.
func TestExtractTexAnnotations_RawTextMathDivDisplay(t *testing.T) {
	html := `<div><p>Definition:</p><div class="math">$$K_{SS}(w) = J_S(w) J_S(w)^\top$$</div><p>more</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `$$K_{SS}(w) = J_S(w) J_S(w)^\top$$`
	if !strings.Contains(got, want) {
		t.Errorf("expected display math\n  want substring: %q\n  got: %q", want, got)
	}
	if strings.Contains(got, `\\top`) || strings.Contains(got, `\_{`) {
		t.Errorf("LaTeX must not be markdown-escaped, got: %q", got)
	}
}

func TestExtractTexAnnotations_RawTextMathSpanInline(t *testing.T) {
	html := `<div><p>Decompose <span class="math">\(g\)</span> along eigenvectors <span class="math">\(v_i\)</span> of <span class="math">\(K_{SS}\)</span>.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `Decompose $g$ along eigenvectors $v_i$ of $K_{SS}$.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExtractTexAnnotations_RawTextMathBracketDisplay(t *testing.T) {
	html := `<div><p>Then:</p><div class="math">\[\partial_t u = -K_{SS} g\]</div></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `$$\partial_t u = -K_{SS} g$$`
	if !strings.Contains(got, want) {
		t.Errorf("expected display math\n  want substring: %q\n  got: %q", want, got)
	}
}

// Containers whose body isn't a single math expression should pass through
// untouched (no false positives on stray words containing "math").
func TestExtractTexAnnotations_NonMathClassMathPassthrough(t *testing.T) {
	html := `<div><p>This <span class="math">discussion</span> is not math.</p></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ExtractMarkdown(doc.Selection)
	want := `This discussion is not math.`
	if got != want {
		t.Errorf("ExtractMarkdown\n  got:  %q\n  want: %q", got, want)
	}
}
