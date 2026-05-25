package pdfextract

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractTextPages_Digital(t *testing.T) {
	pdf, err := os.ReadFile("testdata/digital.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pages, err := extractTextPages(pdf)
	if err != nil {
		t.Fatalf("extractTextPages: %v", err)
	}
	if len(pages) < 1 {
		t.Fatalf("expected at least 1 page, got %d", len(pages))
	}
	allText := strings.Join(pages, "\n")
	if utf8.RuneCountInString(allText) < MinTextForDigital {
		t.Fatalf("expected ≥%d runes of text, got %d", MinTextForDigital, utf8.RuneCountInString(allText))
	}
	if !strings.Contains(allText, "quick brown fox") {
		t.Fatalf("expected 'quick brown fox' in extracted text, got: %q", allText)
	}
}

func TestExtractTextPages_MultiplePages(t *testing.T) {
	pdf, err := os.ReadFile("testdata/mixed.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pages, err := extractTextPages(pdf)
	if err != nil {
		t.Fatalf("extractTextPages: %v", err)
	}
	if len(pages) < 2 {
		t.Fatalf("expected ≥2 pages from mixed.pdf, got %d", len(pages))
	}
}

func TestExtractTextPages_Scanned(t *testing.T) {
	pdf, err := os.ReadFile("testdata/scanned.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pages, err := extractTextPages(pdf)
	if err != nil {
		t.Fatalf("extractTextPages: %v", err)
	}
	allText := strings.Join(pages, "")
	if utf8.RuneCountInString(allText) >= MinTextForDigital {
		t.Fatalf("scanned.pdf should yield <%d runes, got %d", MinTextForDigital, utf8.RuneCountInString(allText))
	}
}

func TestExtractTextPages_Corrupt(t *testing.T) {
	pdf, err := os.ReadFile("testdata/corrupt.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if _, err := extractTextPages(pdf); err == nil {
		t.Fatal("expected error from corrupt PDF, got nil")
	}
}
