package pdfextract

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runCmd runs an external binary with the given args and optional stdin,
// returning stdout. Stderr is captured and included in the wrapped error
// on non-zero exit. The caller is responsible for the context's
// cancellation/timeout — pass context.Background() for indefinite waits.
func runCmd(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w (stderr: %s)", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
