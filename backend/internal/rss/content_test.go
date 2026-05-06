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
}
