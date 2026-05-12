package rss

import (
	"os"
	"strings"
	"testing"
)

func TestExtractCandidates_AwesomeList(t *testing.T) {
	data, err := os.ReadFile("testdata/linkset/awesome_list.html")
	if err != nil {
		t.Fatal(err)
	}
	cands := ExtractCandidates(string(data), "https://github.com/example/awesome-go")

	if len(cands) != 3 {
		t.Fatalf("want 3 candidates (3 valid github links, 3 rejected); got %d: %+v", len(cands), cands)
	}
	if cands[0].Title != "gin" || cands[0].URL != "https://github.com/gin-gonic/gin" {
		t.Errorf("candidate[0] = %+v", cands[0])
	}
	if !strings.Contains(cands[0].EditorNote, "HTTP web framework") {
		t.Errorf("expected editor note for gin, got %q", cands[0].EditorNote)
	}
	if cands[1].Title != "Echo" {
		t.Errorf("candidate[1].Title = %q", cands[1].Title)
	}
}

func TestExtractCandidates_RejectsNoise(t *testing.T) {
	html := `<html><body>
        <a href="https://example.com/article">Real article</a>
        <a href="">empty</a>
        <a href="mailto:a@b">mail</a>
        <a href="javascript:void(0)">js</a>
        <a href="#sec">anchor</a>
        <a href="https://twitter.com/intent/tweet?url=x">share</a>
        <a href="https://parent.example/unsubscribe">unsub</a>
        <a href="https://example.com/article">dup of first</a>
        <a href="here">here</a>
    </body></html>`
	cands := ExtractCandidates(html, "https://parent.example/issue/1")
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate, got %d: %+v", len(cands), cands)
	}
	if cands[0].URL != "https://example.com/article" {
		t.Errorf("URL = %q", cands[0].URL)
	}
}

func TestExtractCandidates_NormalisesURL(t *testing.T) {
	html := `<a href="https://Example.COM/Article/?utm_source=newsletter&utm_medium=email&ref=foo#section">Article</a>`
	cands := ExtractCandidates(html, "https://parent.example/issue/1")
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(cands))
	}
	if cands[0].URL != "https://example.com/Article" {
		t.Errorf("normalised URL = %q; want lowercased host, no trailing slash, no utm_*, no fragment", cands[0].URL)
	}
}

func TestExtractCandidates_ExcludesParentHost(t *testing.T) {
	html := `
        <a href="/internal-link">internal</a>
        <a href="https://parent.example/another-page">also internal</a>
        <a href="https://sub.parent.example/x">subdomain</a>
        <a href="https://other.example/x">external</a>
    `
	cands := ExtractCandidates(html, "https://parent.example/issue/1")
	if len(cands) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(cands), cands)
	}
	if cands[0].URL != "https://other.example/x" {
		t.Errorf("URL = %q", cands[0].URL)
	}
}

func TestExtractCandidates_BlurbFromListItem(t *testing.T) {
	html := `<ul><li><a href="https://example.com/a">Title</a> — a short blurb about the article.</li></ul>`
	cands := ExtractCandidates(html, "https://p.example/x")
	if len(cands) != 1 {
		t.Fatal("want 1 candidate")
	}
	if !strings.Contains(cands[0].EditorNote, "short blurb") {
		t.Errorf("editor note = %q", cands[0].EditorNote)
	}
}

func TestExtractCandidates_LongBlurbCapped(t *testing.T) {
	long := strings.Repeat("x ", 200)
	html := `<ul><li><a href="https://example.com/a">T</a> ` + long + `</li></ul>`
	cands := ExtractCandidates(html, "https://p.example/x")
	if len(cands) != 1 {
		t.Fatal("want 1")
	}
	if len([]rune(cands[0].EditorNote)) > 280 {
		t.Errorf("editor note not capped: %d runes", len([]rune(cands[0].EditorNote)))
	}
}
