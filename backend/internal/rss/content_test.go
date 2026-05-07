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
