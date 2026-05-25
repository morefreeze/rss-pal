package pdfextract

import (
	"context"
	"strings"
)

// extractTextPages runs `pdftotext -layout` on the supplied PDF bytes and
// splits the output on form feed (\f), which pdftotext emits between
// pages by default. The last page may or may not have a trailing \f; we
// drop a trailing empty page so the slice length matches the page count.
func extractTextPages(ctx context.Context, pdfBytes []byte) ([]string, error) {
	out, err := runCmd(ctx, "pdftotext", []string{"-layout", "-", "-"}, pdfBytes)
	if err != nil {
		return nil, err
	}
	// Form feed (\f, 0x0C) is the page separator. The last page may or
	// may not have a trailing \f; strings.Split handles both cleanly.
	raw := strings.Split(string(out), "\f")
	pages := make([]string, 0, len(raw))
	for _, p := range raw {
		trimmed := strings.TrimRight(p, " \t\n\r")
		// Keep empty pages — caller (markdown.go) decides whether to skip.
		pages = append(pages, trimmed)
	}
	// Drop a trailing empty page that's an artifact of a final \f.
	if len(pages) > 0 && pages[len(pages)-1] == "" {
		pages = pages[:len(pages)-1]
	}
	return pages, nil
}
