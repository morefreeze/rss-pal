package pdfextract

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// extractTextPages runs `pdftotext -layout` on the supplied PDF bytes and
// splits the output on form feed (\f), which pdftotext emits between
// pages by default. The last page may or may not have a trailing \f; we
// drop a trailing empty page so the slice length matches the page count.
func extractTextPages(pdfBytes []byte) ([]string, error) {
	cmd := exec.Command("pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(pdfBytes)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftotext: %w (stderr: %s)", err, strings.TrimSpace(errBuf.String()))
	}
	// Form feed (\f, 0x0C) is the page separator. The last page may or
	// may not have a trailing \f; strings.Split handles both cleanly.
	raw := strings.Split(out.String(), "\f")
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
