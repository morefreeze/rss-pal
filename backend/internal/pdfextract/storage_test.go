package pdfextract

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteImages_AndRemoveDir(t *testing.T) {
	base := t.TempDir()
	imgs := []ImageRef{
		{Idx: 0, Format: "png", Bytes: []byte("fake-png-bytes")},
		{Idx: 1, Format: "jpg", Bytes: []byte("fake-jpg-bytes")},
		// Cover the two-digit case to lock in strconv-based naming.
		{Idx: 12, Format: "png", Bytes: []byte("another-png")},
	}
	if err := WriteImages(base, 7, imgs); err != nil {
		t.Fatalf("WriteImages: %v", err)
	}
	for _, im := range imgs {
		p := filepath.Join(base, "article_images", "7", strconv.Itoa(im.Idx)+"."+im.Format)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing file %s: %v", p, err)
		}
	}
	if err := RemoveImageDir(base, 7); err != nil {
		t.Fatalf("RemoveImageDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "article_images", "7")); !os.IsNotExist(err) {
		t.Errorf("dir should be gone, got err=%v", err)
	}
}

func TestRemoveImageDir_Missing_NoError(t *testing.T) {
	base := t.TempDir()
	// Removing a non-existent dir should be a no-op.
	if err := RemoveImageDir(base, 999); err != nil {
		t.Fatalf("RemoveImageDir on missing dir: %v", err)
	}
}

func TestImagePath_Format(t *testing.T) {
	got := ImagePath("/tmp/base", 42, 3, "jpg")
	want := "/tmp/base/article_images/42/3.jpg"
	if got != want {
		t.Errorf("ImagePath: got %q want %q", got, want)
	}
}

func TestImagePath_TwoDigitIdx(t *testing.T) {
	got := ImagePath("/tmp/base", 1, 12, "png")
	want := "/tmp/base/article_images/1/12.png"
	if got != want {
		t.Errorf("ImagePath two-digit idx: got %q want %q", got, want)
	}
}
