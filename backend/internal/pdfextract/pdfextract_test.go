package pdfextract

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExtractFast_Digital(t *testing.T) {
	pdf, err := os.ReadFile("testdata/digital.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	r, err := ExtractFast(context.Background(), pdf)
	if err != nil {
		t.Fatalf("ExtractFast: %v", err)
	}
	if len(r.Pages) < 1 {
		t.Fatalf("expected ≥1 page, got %d", len(r.Pages))
	}
	BuildMarkdown(&r, 99)
	if !strings.Contains(r.Markdown, "## 第 1 页") {
		t.Errorf("expected page heading in markdown")
	}
}

func TestExtractFast_Scanned_ReturnsErrNoText(t *testing.T) {
	pdf, err := os.ReadFile("testdata/scanned.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	r, err := ExtractFast(context.Background(), pdf)
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("expected ErrNoText, got %v", err)
	}
	// Partial Result is OK — caller decides what to do with it.
	if len(r.Pages) == 0 {
		t.Log("partial result has 0 pages — acceptable")
	}
}

func TestExtractFast_KnuthFixture(t *testing.T) {
	pdf, err := os.ReadFile("testdata/knuth-1980.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	r, err := ExtractFast(context.Background(), pdf)
	if err != nil {
		t.Fatalf("ExtractFast knuth: %v", err)
	}
	if len(r.Pages) < 5 {
		t.Fatalf("expected knuth-1980.pdf to have many pages, got %d", len(r.Pages))
	}
	BuildMarkdown(&r, 1)
	if len(r.Markdown) < MinTextForDigital {
		t.Fatalf("expected substantial markdown, got %d runes", len(r.Markdown))
	}
}
