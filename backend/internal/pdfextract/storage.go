package pdfextract

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// imageDir returns the directory holding an article's PDF images.
func imageDir(base string, articleID int) string {
	return filepath.Join(base, "article_images", strconv.Itoa(articleID))
}

// ImagePath returns the absolute path for one image file.
func ImagePath(base string, articleID, idx int, format string) string {
	return filepath.Join(imageDir(base, articleID), strconv.Itoa(idx)+"."+format)
}

// WriteImages mkdir-p's the article's image dir and writes all images.
// Existing files at the same path are overwritten.
func WriteImages(base string, articleID int, imgs []ImageRef) error {
	dir := imageDir(base, articleID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	for _, im := range imgs {
		p := ImagePath(base, articleID, im.Idx, im.Format)
		if err := os.WriteFile(p, im.Bytes, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

// RemoveImageDir deletes an article's entire image directory. Safe to
// call when the dir doesn't exist (returns nil).
func RemoveImageDir(base string, articleID int) error {
	dir := imageDir(base, articleID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("rm %s: %w", dir, err)
	}
	return nil
}
