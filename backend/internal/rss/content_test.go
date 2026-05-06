package rss

import (
	"strings"
	"testing"
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
