package pdfextract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// extractImages shells out to `pdfimages -all` to dump every embedded
// image of the supplied PDF into a temp directory, then loads each file
// back in, dedupes by SHA-256, and caps the kept set at MaxImagesPerPDF.
// Files are processed in pdfimages emission order (lexical filename
// sort, which matches page order). Returns the kept images, the
// pre-dedup count (including duplicates), and any error.
func extractImages(pdfBytes []byte) ([]ImageRef, int, error) {
	tmpDir, err := os.MkdirTemp("", "pdfextract-images-")
	if err != nil {
		return nil, 0, fmt.Errorf("mkdir tmp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		return nil, 0, fmt.Errorf("write pdf to tmp: %w", err)
	}

	prefix := filepath.Join(tmpDir, "img")
	cmd := exec.Command("pdfimages", "-all", pdfPath, prefix)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, 0, fmt.Errorf("pdfimages: %w (stderr: %s)", err, strings.TrimSpace(errBuf.String()))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, 0, fmt.Errorf("read tmpdir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "input.pdf" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	kept := make([]ImageRef, 0, len(names))
	seen := make(map[string]bool, len(names))
	totalOriginal := 0

	for _, name := range names {
		totalOriginal++
		data, err := os.ReadFile(filepath.Join(tmpDir, name))
		if err != nil {
			return nil, 0, fmt.Errorf("read image %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hexSum := hex.EncodeToString(sum[:])
		if seen[hexSum] {
			continue
		}
		if len(kept) >= MaxImagesPerPDF {
			continue
		}
		seen[hexSum] = true
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if format == "jpeg" {
			format = "jpg"
		}
		kept = append(kept, ImageRef{
			Idx:    len(kept),
			Bytes:  data,
			Format: format,
			SHA256: hexSum,
		})
	}

	return kept, totalOriginal, nil
}
