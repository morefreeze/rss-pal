package pdfextract

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	ocrLangsOnce sync.Once
	ocrLangs     string
)

// chooseOCRLangs returns the preferred Tesseract -l value based on
// what's installed locally. Falls back to "eng" alone if chi_sim is
// unavailable (host dev environments often don't have it). Production
// Docker images install both chi_sim + eng.
//
// chooseOCRLangs is cached: tesseract's installed language data doesn't
// change without a process restart, and on a multi-page OCR job we'd
// otherwise fork tesseract once per page just to probe.
func chooseOCRLangs(ctx context.Context) string {
	ocrLangsOnce.Do(func() {
		// tesseract --list-langs writes to stderr on some versions, stdout
		// on others; CombinedOutput-style would be safer, but runCmd
		// captures stderr-on-error. Try runCmd first; if it fails, fall
		// back to "eng" to keep the call infallible by contract.
		out, err := runCmd(ctx, "tesseract", []string{"--list-langs"}, nil)
		if err != nil {
			ocrLangs = "eng"
			return
		}
		s := string(out)
		hasChi := strings.Contains(s, "chi_sim")
		hasEng := strings.Contains(s, "eng")
		switch {
		case hasChi && hasEng:
			ocrLangs = "chi_sim+eng"
		case hasEng:
			ocrLangs = "eng"
		case hasChi:
			ocrLangs = "chi_sim"
		default:
			ocrLangs = "eng"
		}
	})
	return ocrLangs
}

// extractWithOCR renders each page of the PDF to a 300 dpi PNG via
// pdftoppm, then OCRs each PNG via tesseract. It also runs extractImages
// so we still get embedded raster images even for scanned PDFs.
// Per-page OCR failures get inlined into the page text as a marker line
// and the extraction still succeeds; only a pipeline-level failure
// (e.g. pdftoppm can't open the PDF) returns a non-nil error.
//
// Replaces the stub ExtractWithOCR in pdfextract.go via init wiring —
// kept in a separate file so ocr_test.go is naturally co-located.
func extractWithOCR(ctx context.Context, pdfBytes []byte) (Result, error) {
	var r Result

	title, err := extractTitle(ctx, pdfBytes)
	if err != nil {
		return r, fmt.Errorf("title: %w", err)
	}
	r.Title = title

	tmpDir, err := os.MkdirTemp("", "pdfextract-ocr-")
	if err != nil {
		return r, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "in.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		return r, fmt.Errorf("write pdf: %w", err)
	}

	pagePrefix := filepath.Join(tmpDir, "page")
	if _, err := runCmd(ctx, "pdftoppm", []string{"-r", "300", "-png", pdfPath, pagePrefix}, nil); err != nil {
		return r, err
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return r, fmt.Errorf("read tmpdir: %w", err)
	}
	var pngs []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".png" {
			pngs = append(pngs, e.Name())
		}
	}
	sort.Strings(pngs)

	langs := chooseOCRLangs(ctx)

	pages := make([]PageContent, 0, len(pngs))
	for i, name := range pngs {
		pageNum := i + 1
		imgPath := filepath.Join(tmpDir, name)
		stdout, err := runCmd(ctx, "tesseract", []string{imgPath, "-", "-l", langs}, nil)
		if err != nil {
			msg := strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(err.Error()))
			if len(msg) > 200 {
				msg = msg[:200] + "…"
			}
			pages = append(pages, PageContent{
				PageNum: pageNum,
				Text:    fmt.Sprintf("> [第 %d 页 OCR 失败：%s]", pageNum, msg),
			})
			continue
		}
		pages = append(pages, PageContent{
			PageNum: pageNum,
			Text:    strings.TrimRight(string(stdout), " \t\n\r"),
		})
	}
	r.Pages = pages

	// Also extract embedded raster images so scanned PDFs with figures
	// still get the figures in markdown.
	imgs, total, err := extractImages(ctx, pdfBytes)
	if err == nil {
		r.TotalImagesOriginal = total
		r.TotalImagesKept = len(imgs)
		r.Pages = distributeImages(r.Pages, imgs)
	}

	return r, nil
}
