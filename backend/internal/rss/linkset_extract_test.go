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

func TestExtractCandidates_ButtondownIssueSmoke(t *testing.T) {
	data, err := os.ReadFile("testdata/linkset/buttondown_issue.html")
	if err != nil {
		t.Fatal(err)
	}
	cands := ExtractCandidates(string(data), "https://buttondown.com/hacker-newsletter/archive/793/")

	if len(cands) < 5 {
		t.Fatalf("expected ≥5 candidates from a real Buttondown issue, got %d", len(cands))
	}

	// None should be social-share / unsubscribe noise.
	for _, c := range cands {
		if strings.Contains(c.URL, "t.co/") {
			t.Errorf("t.co link should be filtered: %s", c.URL)
		}
		if strings.Contains(c.URL, "/unsubscribe") {
			t.Errorf("unsubscribe link should be filtered: %s", c.URL)
		}
		if strings.Contains(c.URL, "twitter.com/intent") ||
			strings.Contains(c.URL, "twitter.com/share") {
			t.Errorf("twitter share/intent should be filtered: %s", c.URL)
		}
	}

	// No buttondown.com self-links (newsletter root / emails/).
	for _, c := range cands {
		if strings.HasPrefix(c.URL, "https://buttondown.com/hacker-newsletter") ||
			strings.HasPrefix(c.URL, "https://buttondown.com/emails/") {
			t.Errorf("buttondown self-link should be filtered: %s", c.URL)
		}
	}

	// URLs are deduped (no duplicates after normalisation).
	seen := map[string]struct{}{}
	for _, c := range cands {
		if _, dup := seen[c.URL]; dup {
			t.Errorf("duplicate URL: %s", c.URL)
		}
		seen[c.URL] = struct{}{}
	}
}

func TestExtractCandidates_RejectsCommentsLinks(t *testing.T) {
	// Newsletter pattern: each article is paired with a "comments→" link to
	// the discussion page. The comments link should be filtered as stopword
	// navigation; the article link kept.
	html := `<ul>
        <li><a href="https://example.com/article">Real Article Title</a>
            <a href="https://news.ycombinator.com/item?id=123">comments→</a></li>
        <li><a href="https://example.com/article2">Second Article</a>
            <a href="https://news.ycombinator.com/item?id=124">comments &rarr;</a></li>
        <li><a href="https://example.com/article3">Third</a>
            <a href="https://news.ycombinator.com/item?id=125">discuss</a></li>
    </ul>`
	cands := ExtractCandidates(html, "https://buttondown.com/foo/archive/1")
	if len(cands) != 3 {
		t.Fatalf("want 3 real-article candidates (comments/discuss filtered), got %d: %+v", len(cands), cands)
	}
	for _, c := range cands {
		if c.URL != "" && !strings.HasPrefix(c.URL, "https://example.com/") {
			t.Errorf("unexpected candidate URL %q (should only be example.com articles)", c.URL)
		}
	}
}

func TestExtractCandidates_SameHostSameDepthAllowed(t *testing.T) {
	// Multi-tenant platforms (github.com, dev.to) host many independent
	// resources at sibling paths. A link from /a/b/awesome to /a/b/library
	// (same depth) must NOT be filtered as "own-site".
	html := `<a href="https://github.com/foo/lib-a">lib-a</a>`
	cands := ExtractCandidates(html, "https://github.com/foo/awesome-list")
	if len(cands) != 1 {
		t.Fatalf("same-host same-depth link should pass; got %d candidates", len(cands))
	}
	if cands[0].URL != "https://github.com/foo/lib-a" {
		t.Errorf("URL = %q", cands[0].URL)
	}
}
