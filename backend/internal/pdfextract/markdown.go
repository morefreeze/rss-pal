package pdfextract

import (
	"fmt"
	"strings"
)

// assembleMarkdown stitches per-page text + image refs into the final
// article body. articleID is needed for image URLs. totalOriginal and
// totalKept drive the optional footer when the 100-image cap fired.
//
// Page-level invariants:
//   - A page with no text AND no images is skipped (no heading)
//   - A page with only text or only images still gets its heading
//   - Images render after the page's text, in slice order
func assembleMarkdown(pages []PageContent, articleID, totalImagesOriginal, totalImagesKept int) string {
	var b strings.Builder
	for _, p := range pages {
		hasText := strings.TrimSpace(p.Text) != ""
		hasImages := len(p.Images) > 0
		if !hasText && !hasImages {
			continue
		}
		fmt.Fprintf(&b, "## 第 %d 页\n\n", p.PageNum)
		if hasText {
			b.WriteString(p.Text)
			b.WriteString("\n\n")
		}
		for _, img := range p.Images {
			fmt.Fprintf(&b, "![](/api/articles/%d/images/%d.%s)\n", articleID, img.Idx, img.Format)
		}
		if hasImages {
			b.WriteString("\n")
		}
	}
	if totalImagesOriginal > totalImagesKept {
		dropped := totalImagesOriginal - totalImagesKept
		fmt.Fprintf(&b,
			"\n> 注：原 PDF 共 %d 张图（去重后 %d 张），超出 %d 张限制，已省略后 %d 张。\n",
			totalImagesOriginal, totalImagesKept, MaxImagesPerPDF, dropped)
	}
	return b.String()
}

// BuildMarkdown is the public wrapper around assembleMarkdown, intended
// for callers that needed to insert the row first to learn the article
// ID. Sets result.Markdown in-place.
func BuildMarkdown(r *Result, articleID int) {
	r.Markdown = assembleMarkdown(r.Pages, articleID, r.TotalImagesOriginal, r.TotalImagesKept)
}
