package pdfextract

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

// extractTitle returns the PDF's /Title metadata, or "" if none. Returns
// a non-nil error when pdfinfo fails (corrupt PDF, missing binary).
func extractTitle(ctx context.Context, pdfBytes []byte) (string, error) {
	out, err := runCmd(ctx, "pdfinfo", []string{"-"}, pdfBytes)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Title:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Title:")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan pdfinfo output: %w", err)
	}
	return "", nil
}
