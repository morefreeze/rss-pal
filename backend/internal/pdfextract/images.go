package pdfextract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
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
func listImagePages(ctx context.Context, pdfPath string) ([]int, error) {
	out, err := runCmd(ctx, "pdfimages", []string{"-list", pdfPath}, nil)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
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
func extractImages(ctx context.Context, pdfBytes []byte) ([]ImageRef, int, error) {
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
	pagePerEmit, err := listImagePages(ctx, pdfPath)
	if err != nil {
		return nil, 0, err
	}

	prefix := filepath.Join(tmpDir, "img")
	// -png (vs -all) forces non-JPEG raster output to PNG. -all preserves
	// the source codec — fine for PNG/JPEG but academic PDFs frequently
	// embed JBIG2 (monochrome scans) or JPX (JPEG 2000), neither of which
	// browsers render. -png converts JBIG2/JPX/PPM → PNG while leaving
	// embedded JPEG untouched, so the on-disk set is always {png, jpg}.
	if _, err := runCmd(ctx, "pdfimages", []string{"-png", pdfPath, prefix}, nil); err != nil {
		return nil, 0, err
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
		if isLikelyPageScan(width, height) {
			// Scanned-PDF style: every page is a 2475x3300+ raster
			// "background" with the OCR text laid over it. pdfimages
			// extracts these as full-page images that are not figures
			// the user wants in the article body. Drop them but DO
			// count them toward totalOriginal so the truncation
			// footer reports the real source-image count.
			continue
		}
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

// isLikelyPageScan flags images that are almost certainly full-page
// background scans (common in OCR'd PDFs from gwern.net etc.) rather
// than real figures. Heuristic: ≥ 4 MP total pixel area AND both
// dimensions ≥ 1500 px. Typical 300 dpi letter/A4 page scans land at
// 2475×3300 ≈ 8 MP; the largest legitimate embedded figures in
// academic PDFs are rarely > 4 MP with both dims that large.
func isLikelyPageScan(width, height int) bool {
	const minDim = 1500
	const minArea = 4_000_000
	if width < minDim || height < minDim {
		return false
	}
	return width*height >= minArea
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
