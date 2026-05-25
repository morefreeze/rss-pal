// Package pdfextract converts PDF bytes into structured text + images
// suitable for storage as a clipped article.
//
// It shells out to poppler-utils (pdftotext, pdfimages, pdftoppm, pdfinfo)
// and Tesseract via os/exec. Callers should treat extraction as IO-bound
// and avoid running many concurrent extractions on one machine.
package pdfextract

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Result is the full output of one extraction.
type Result struct {
	Title               string        // PDF metadata /Title → filename → URL
	Pages               []PageContent // Per-page text + image refs
	Markdown            string        // Final assembled markdown
	TotalImagesOriginal int           // Before dedup
	TotalImagesKept     int           // After dedup + 100-cap
}

// PageContent is the per-page slice of a Result.
type PageContent struct {
	PageNum int        // 1-based
	Text    string     // Page text, no trailing whitespace
	Images  []ImageRef // In page-order
}

// ImageRef describes one image extracted from the PDF. Bytes are loaded
// into memory; callers persist them via WriteImages.
type ImageRef struct {
	Idx     int    // 0-based unique per article
	PageNum int    // 1-based PDF page this image was extracted from; 0 means unknown (parse failure or list/file length mismatch — caller should fall back to page 1)
	Bytes   []byte // Raw image bytes (PNG or JPEG)
	Format  string // "png" | "jpg"
	SHA256  string // Lowercase hex, for ETag + dedup
	Width   int    // Pixels; 0 if unknown
	Height  int    // Pixels; 0 if unknown
}

// MaxImagesPerPDF caps how many images one article retains after dedup.
// Spec'd at 100; defined as a const so tests can lock to it.
const MaxImagesPerPDF = 100

// MinTextForDigital is the threshold below which ExtractFast considers a
// PDF "likely scanned" and the caller should fall back to ExtractWithOCR.
const MinTextForDigital = 200

// ErrNoText is returned by ExtractFast when fewer than MinTextForDigital
// runes of text are extracted across all pages. Callers should queue the
// PDF for async OCR via ExtractWithOCR.
var ErrNoText = errors.New("pdfextract: fewer than MinTextForDigital runes extracted; likely scanned")

// ExtractFast runs pdftotext + pdfimages + pdfinfo (no OCR). Returns
// ErrNoText (alongside a partial Result) when text is too sparse — caller
// should treat this as "queue for OCR" rather than a hard error.
//
// The returned Result.Markdown is intentionally empty — the caller calls
// BuildMarkdown(result, articleID) after it has an article ID. This keeps
// the pure function testable.
func ExtractFast(ctx context.Context, pdfBytes []byte) (Result, error) {
	var r Result

	// Title is best-effort. If pdfinfo fails the PDF is likely corrupt;
	// surface that as a hard error rather than silently continuing.
	title, err := extractTitle(ctx, pdfBytes)
	if err != nil {
		return r, fmt.Errorf("title: %w", err)
	}
	r.Title = title

	textPages, err := extractTextPages(ctx, pdfBytes)
	if err != nil {
		return r, fmt.Errorf("text: %w", err)
	}

	imgs, totalOriginal, err := extractImages(ctx, pdfBytes)
	if err != nil {
		// Image extraction failure is non-fatal — keep going with text-only.
		// We don't want a single bad image to block the whole article.
		imgs = nil
		totalOriginal = 0
	}
	r.TotalImagesOriginal = totalOriginal
	r.TotalImagesKept = len(imgs)

	r.Pages = make([]PageContent, len(textPages))
	for i, tp := range textPages {
		r.Pages[i] = PageContent{PageNum: i + 1, Text: tp}
	}
	r.Pages = distributeImages(r.Pages, imgs)

	// Decide digital-vs-scanned threshold.
	var totalRunes int
	for _, p := range r.Pages {
		totalRunes += utf8.RuneCountInString(p.Text)
	}
	if totalRunes < MinTextForDigital {
		return r, ErrNoText
	}
	return r, nil
}

// ExtractWithOCR is the slow path: renders pages with pdftoppm at 300 dpi
// then OCRs each with Tesseract (chi_sim+eng when available, falling back
// to eng-only). Page-level failures are recorded inline in the page text
// as "> [第 N 页 OCR 失败：<err>]" but do not fail the whole extraction;
// only a pipeline-level failure (e.g. pdftoppm crash) returns a non-nil
// error.
func ExtractWithOCR(ctx context.Context, pdfBytes []byte) (Result, error) {
	return Result{}, errors.New("not implemented")
}

// distributeImages attaches each image to its source page based on
// ImageRef.PageNum (1-based). Images with PageNum < 1 or > len(pages)
// fall back to page 1.
func distributeImages(pages []PageContent, imgs []ImageRef) []PageContent {
	if len(pages) == 0 {
		return pages
	}
	for i := range imgs {
		pn := imgs[i].PageNum
		if pn < 1 || pn > len(pages) {
			pn = 1
		}
		pages[pn-1].Images = append(pages[pn-1].Images, imgs[i])
	}
	return pages
}
