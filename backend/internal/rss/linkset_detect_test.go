package rss

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDetectLinkSetSuggestion_SmallAwesomeFixture(t *testing.T) {
	// The bundled awesome fixture is intentionally tiny (≤3 candidates).
	// It should fail the new stricter rule.
	data, err := os.ReadFile("testdata/linkset/awesome_list.html")
	if err != nil {
		t.Fatal(err)
	}
	if cands, ok := DetectLinkSetSuggestion(string(data), "https://github.com/example/awesome-go"); ok {
		t.Fatalf("small awesome fixture should not qualify under stricter rule; got %d", len(cands))
	}
}

func TestDetectLinkSetSuggestion_Buttondown(t *testing.T) {
	data, err := os.ReadFile("testdata/linkset/buttondown_issue.html")
	if err != nil {
		t.Fatal(err)
	}
	cands, ok := DetectLinkSetSuggestion(string(data), "https://buttondown.com/hacker-newsletter/archive/793/")
	if !ok {
		t.Fatalf("expected hacker-newsletter issue to qualify; got %d candidates", len(cands))
	}
	if len(cands) < LinkSetSuggestionMinCandidates {
		t.Fatalf("buttondown qualifying run too short: %d", len(cands))
	}
}

func TestDetectLinkSetSuggestion_TooFew(t *testing.T) {
	html := `<ul>` + strings.Repeat(`<li><a href="https://example.com/x">x</a></li>`, 5) + `</ul>`
	if cands, ok := DetectLinkSetSuggestion(html, "https://parent.example/"); ok {
		t.Fatalf("5 links should not qualify; got %d", len(cands))
	}
}

func TestDetectLinkSetSuggestion_GapTooBig(t *testing.T) {
	// 11 links but with 3 separate gap segments — should NOT qualify
	// (cap is 2 segments). Layout: cand, gap, cand, gap, cand, gap, cand+8more.
	var b strings.Builder
	b.WriteString(`<ul>`)
	b.WriteString(`<li><a href="https://example.com/1">1</a></li>`)
	b.WriteString(`<li>gap-a</li>`)
	b.WriteString(`<li><a href="https://example.com/2">2</a></li>`)
	b.WriteString(`<li>gap-b</li>`)
	b.WriteString(`<li><a href="https://example.com/3">3</a></li>`)
	b.WriteString(`<li>gap-c</li>`)
	for i := 4; i <= 11; i++ {
		fmt.Fprintf(&b, `<li><a href="https://example.com/%d">%d</a></li>`, i, i)
	}
	b.WriteString(`</ul>`)
	if cands, ok := DetectLinkSetSuggestion(b.String(), "https://parent.example/"); ok {
		t.Fatalf("3 gap segments should disqualify; got %d", len(cands))
	}
}

func TestDetectLinkSetSuggestion_GapsAllowed(t *testing.T) {
	// 11 links with exactly 2 gap segments — should qualify.
	var b strings.Builder
	b.WriteString(`<ul>`)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, `<li><a href="https://example.com/%d">%d</a></li>`, i, i)
	}
	b.WriteString(`<li>gap-a</li>`)
	for i := 6; i <= 9; i++ {
		fmt.Fprintf(&b, `<li><a href="https://example.com/%d">%d</a></li>`, i, i)
	}
	b.WriteString(`<li>gap-b</li>`)
	for i := 10; i <= 11; i++ {
		fmt.Fprintf(&b, `<li><a href="https://example.com/%d">%d</a></li>`, i, i)
	}
	b.WriteString(`</ul>`)
	cands, ok := DetectLinkSetSuggestion(b.String(), "https://parent.example/")
	if !ok {
		t.Fatalf("11 candidates with 2 gap segments should qualify; got %d", len(cands))
	}
	if len(cands) != 11 {
		t.Fatalf("expected 11 candidates in run, got %d", len(cands))
	}
}
