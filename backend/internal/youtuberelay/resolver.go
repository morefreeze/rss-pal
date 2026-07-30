package youtuberelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"
)

const maxResolverOutput = 16 << 20

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type YTDLPResolver struct {
	Runner         CommandRunner
	Binary         string
	Timeout        time.Duration
	POTProviderURL string
}

func NewYTDLPResolver() *YTDLPResolver {
	return &YTDLPResolver{
		Runner:         execCommandRunner{},
		Binary:         "yt-dlp",
		Timeout:        45 * time.Second,
		POTProviderURL: os.Getenv("YOUTUBE_POT_PROVIDER_URL"),
	}
}

func (r YTDLPResolver) Resolve(ctx context.Context, videoID string) (ResolvedMedia, error) {
	if !youtubeIDPattern.MatchString(videoID) {
		return ResolvedMedia{}, ErrInvalidVideoID
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := r.Runner
	if runner == nil {
		runner = execCommandRunner{}
	}
	binary := r.Binary
	if binary == "" {
		binary = "yt-dlp"
	}
	args := []string{
		"--no-warnings",
		"--no-playlist",
		"--skip-download",
		"--socket-timeout", "20",
		"--js-runtimes", "deno",
	}
	if r.POTProviderURL != "" {
		args = append(args,
			"--extractor-args", "youtube:player_client=mweb",
			"--extractor-args", "youtubepot-bgutilhttp:base_url="+r.POTProviderURL,
		)
	}
	args = append(args,
		"-J",
		"https://www.youtube.com/watch?v="+videoID,
	)
	output, err := runner.Output(ctx, binary, args...)
	if err != nil {
		return ResolvedMedia{}, fmt.Errorf("%w: %v", ErrResolveFailed, err)
	}
	if len(output) == 0 || len(output) > maxResolverOutput {
		return ResolvedMedia{}, fmt.Errorf("%w: invalid output size", ErrResolveFailed)
	}

	var info VideoInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return ResolvedMedia{}, fmt.Errorf("%w: invalid json", ErrResolveFailed)
	}
	if info.ID != videoID || info.Duration <= 0 {
		return ResolvedMedia{}, fmt.Errorf("%w: mismatched metadata", ErrResolveFailed)
	}
	selection, err := SelectFormats(info, MaxCombinedKbps)
	if err != nil {
		if errors.Is(err, ErrNoCompatibleMedia) {
			return ResolvedMedia{}, err
		}
		return ResolvedMedia{}, fmt.Errorf("%w: selection", ErrResolveFailed)
	}
	return ResolvedMedia{Info: info, Selection: selection}, nil
}

type execCommandRunner struct{}

func (execCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout cappedBuffer
	stdout.max = maxResolverOutput
	var stderr cappedBuffer
	stderr.max = 64 << 10

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		remaining := b.max - b.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return 0, errors.New("command output exceeds limit")
	}
	return b.Buffer.Write(p)
}
