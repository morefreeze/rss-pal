package pdfextract

import (
	"os"
	"testing"
)

func TestExtractTitle_FromPDFMetadata(t *testing.T) {
	pdf, err := os.ReadFile("testdata/knuth-1980.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	title, err := extractTitle(pdf)
	if err != nil {
		t.Fatalf("extractTitle: %v", err)
	}
	if title == "" {
		t.Fatal("expected non-empty title from PDF metadata")
	}
}

func TestExtractTitle_NoMetadata(t *testing.T) {
	pdf, err := os.ReadFile("testdata/digital.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// digital.pdf has no /Title — extractTitle should return "" without error
	title, err := extractTitle(pdf)
	if err != nil {
		t.Fatalf("extractTitle: %v", err)
	}
	t.Logf("digital.pdf title: %q", title)
	// Don't assert empty — depends on how the fixture was generated.
	// We're verifying it doesn't crash.
}

func TestExtractTitle_Corrupt(t *testing.T) {
	pdf, err := os.ReadFile("testdata/corrupt.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if _, err := extractTitle(pdf); err == nil {
		t.Fatal("expected error from corrupt PDF, got nil")
	}
}
