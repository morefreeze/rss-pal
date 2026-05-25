package pdfextract

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExtractWithOCR_Scanned(t *testing.T) {
	if testing.Short() {
		t.Skip("OCR is slow")
	}
	pdf, err := os.ReadFile("testdata/scanned.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	r, err := ExtractWithOCR(context.Background(), pdf)
	if err != nil {
		t.Fatalf("ExtractWithOCR: %v", err)
	}
	if len(r.Pages) == 0 {
		t.Fatal("expected ≥1 page from OCR")
	}
	// We don't assert OCR accuracy — just that some text came out.
	var total int
	for _, p := range r.Pages {
		total += len(strings.TrimSpace(p.Text))
	}
	if total == 0 {
		t.Fatal("OCR produced zero characters")
	}
	t.Logf("OCR produced %d total chars across %d pages with langs=%s", total, len(r.Pages), chooseOCRLangs())
}

func TestExtractWithOCR_Corrupt(t *testing.T) {
	if testing.Short() {
		t.Skip("OCR is slow")
	}
	pdf, err := os.ReadFile("testdata/corrupt.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if _, err := ExtractWithOCR(context.Background(), pdf); err == nil {
		t.Fatal("expected error on corrupt PDF, got nil")
	}
}
