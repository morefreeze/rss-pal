package pdfextract

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// extractTitle returns the PDF's /Title metadata, or "" if none. Returns
// a non-nil error when pdfinfo fails (corrupt PDF, missing binary).
func extractTitle(pdfBytes []byte) (string, error) {
	cmd := exec.Command("pdfinfo", "-")
	cmd.Stdin = bytes.NewReader(pdfBytes)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdfinfo: %w (stderr: %s)", err, strings.TrimSpace(errBuf.String()))
	}
	scanner := bufio.NewScanner(&out)
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
