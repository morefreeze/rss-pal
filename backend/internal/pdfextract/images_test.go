package pdfextract

import (
	"context"
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
	imgs, totalOriginal, err := extractImages(context.Background(), pdf)
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
		if im.Width <= 0 || im.Height <= 0 {
			t.Errorf("img idx=%d missing dims: %dx%d", im.Idx, im.Width, im.Height)
		}
	}
	if len(imgs) > 0 {
		t.Logf("sample image idx=%d format=%s dims=%dx%d", imgs[0].Idx, imgs[0].Format, imgs[0].Width, imgs[0].Height)
	}
}

func TestExtractImages_PerImagePageNum(t *testing.T) {
	pdf, err := os.ReadFile("testdata/image_heavy.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	imgs, _, err := extractImages(context.Background(), pdf)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	for _, im := range imgs {
		if im.PageNum < 1 {
			t.Fatalf("img idx=%d has bad PageNum=%d", im.Idx, im.PageNum)
		}
	}
}

func TestExtractImages_Digital_HasFew(t *testing.T) {
	pdf, err := os.ReadFile("testdata/digital.pdf")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	imgs, _, err := extractImages(context.Background(), pdf)
	if err != nil {
		t.Fatalf("extractImages: %v", err)
	}
	if len(imgs) > 5 {
		t.Fatalf("digital.pdf is text-only; expected ≤5 images, got %d", len(imgs))
	}
}
