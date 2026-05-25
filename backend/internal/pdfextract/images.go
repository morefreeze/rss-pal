package pdfextract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// listImagePages runs `pdfimages -list <pdfPath>` and returns the
// 1-based page number for each image in pdfimages emission order.
// The output begins with a 2-line header (column names + dashes); we
// skip those and read the first whitespace-delimited field of every
// remaining non-empty line.
//
// We assume pdfimages -list and pdfimages -all emit rows/files in the
// same order — both walk the page tree DFS with one counter as of
// poppler 24.x. If a future version diverges, PageNum will silently
// default to 0.
func listImagePages(pdfPath string) ([]int, error) {
	// TODO(phase-1d): switch to exec.CommandContext when the package-level
	// exec helper lands in Phase 1D.
	cmd := exec.Command("pdfimages", "-list", pdfPath)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdfimages -list: %w (stderr: %s)", err, strings.TrimSpace(errBuf.String()))
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) < 2 {
		return nil, nil
	}
	// Skip the two header lines (column names + dashes).
	rows := lines[2:]
	pages := make([]int, 0, len(rows))
	for _, line := range rows {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			// Non-numeric first field (e.g. an unexpected banner or repeated
			// header line) — skip rather than fail the whole capture.
			continue
		}
		pages = append(pages, n)
	}
	return pages, nil
}

// extractImages shells out to `pdfimages -all` to dump every embedded
// image of the supplied PDF into a temp directory, then loads each file
// back in, dedupes by SHA-256, and caps the kept set at MaxImagesPerPDF.
// Files are processed in pdfimages emission order (lexical filename
// sort, which matches page order). Returns the kept images, the
// pre-dedup count (including duplicates), and any error.
//
// We invoke pdfimages twice (-list then -all) for clarity; combining
// via `pdfimages -all -list` is possible (poppler ≥ 0.32) but couples
// parsing to extraction. Phase-1D perf review can revisit.
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

	// Capture per-image page numbers before emitting files so we can
	// stamp each ImageRef.PageNum. pagePerEmit is indexed by pdfimages
	// emission order (pre-dedup), NOT by the kept ImageRef.Idx.
	pagePerEmit, err := listImagePages(pdfPath)
	if err != nil {
		return nil, 0, err
	}

	prefix := filepath.Join(tmpDir, "img")
	// TODO(phase-1d): switch to exec.CommandContext when the package-level
	// exec helper lands in Phase 1D.
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

	if len(names) != len(pagePerEmit) {
		log.Printf("pdfextract: pdfimages -list/-all length mismatch: files=%d list=%d (PageNum may be 0 for trailing images)", len(names), len(pagePerEmit))
	}

	kept := make([]ImageRef, 0, len(names))
	seen := make(map[string]bool, len(names))
	totalOriginal := 0

	for emit, name := range names {
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
		pageNum := 0
		if emit < len(pagePerEmit) {
			pageNum = pagePerEmit[emit]
		}
		width, height := decodeDimensions(data)
		kept = append(kept, ImageRef{
			Idx:     len(kept),
			PageNum: pageNum,
			Bytes:   data,
			Format:  format,
			SHA256:  hexSum,
			Width:   width,
			Height:  height,
		})
	}

	return kept, totalOriginal, nil
}

// decodeDimensions returns width/height from the image header. Returns
// (0,0) if the image can't be decoded — non-fatal, we just lose the hint.
func decodeDimensions(b []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
