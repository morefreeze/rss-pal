package pdfextract

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestExtractImages_DedupAndCap(t *testing.T) {
	pdf, err := os.ReadFile("testdata/image_heavy.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	imgs, totalOriginal, err := extractImages(pdf)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	t.Logf("image_heavy.pdf: original=%d kept=%d", totalOriginal, len(imgs))
	if totalOriginal <= len(imgs) {
		t.Fatalf("expected dedup to reduce count: original=%d kept=%d", totalOriginal, len(imgs))
	}
	if len(imgs) > MaxImagesPerPDF {
		t.Fatalf("expected ≤%d images, got %d", MaxImagesPerPDF, len(imgs))
	}
	seen := map[string]bool{}
	for _, im := range imgs {
		if seen[im.SHA256] {
			t.Fatalf("dup SHA in kept images: %s", im.SHA256)
		}
		seen[im.SHA256] = true
		h := sha256.Sum256(im.Bytes)
		expected := hex.EncodeToString(h[:])
		if im.SHA256 != expected {
			t.Fatalf("SHA mismatch idx=%d: have %s want %s", im.Idx, im.SHA256, expected)
		}
		if im.Format != "png" && im.Format != "jpg" {
			t.Fatalf("unexpected format: %q", im.Format)
		}
	}
}

func TestExtractImages_Digital_HasFew(t *testing.T) {
	pdf, err := os.ReadFile("testdata/digital.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	imgs, _, err := extractImages(pdf)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	if len(imgs) > 5 {
		t.Fatalf("digital.pdf is text-only; expected ≤5 images, got %d", len(imgs))
	}
}
