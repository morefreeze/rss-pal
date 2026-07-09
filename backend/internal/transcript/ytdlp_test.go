package transcript

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestYTDLPFetchesSubtitleFilesEvenWhenCommandExitsNonZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake is unix-only")
	}
	fake := filepath.Join(t.TempDir(), "fake-ytdlp")
	script := `#!/bin/sh
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--output" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
dir=$(dirname "$out")
cat > "$dir/sub.en.json3" <<'JSON'
{"events":[{"segs":[{"utf8":"Hello "},{"utf8":"world."}]},{"segs":[{"utf8":"Second line."}]}]}
JSON
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	f := &YTDLP{Command: fake, Timeout: 2 * time.Second}

	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected transcript result")
	}
	if !strings.Contains(got.Text, "Hello world.") || !strings.Contains(got.Text, "Second line.") {
		t.Fatalf("unexpected transcript text: %q", got.Text)
	}
	if got.Source != "yt-dlp 字幕" {
		t.Fatalf("source = %q, want yt-dlp 字幕", got.Source)
	}
}

func TestYTDLPContinuesToNextLanguageWhenPreferredLanguageFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake is unix-only")
	}
	fake := filepath.Join(t.TempDir(), "fake-ytdlp")
	script := `#!/bin/sh
lang=""
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--sub-langs" ]; then
    lang="$arg"
  fi
  if [ "$prev" = "--output" ]; then
    out="$arg"
  fi
  prev="$arg"
done
case "$lang" in
*zh-Hans*)
  echo "preferred language failed" >&2
  exit 1
  ;;
esac
dir=$(dirname "$out")
cat > "$dir/sub.$lang.json3" <<'JSON'
{"events":[{"segs":[{"utf8":"English fallback."}]}]}
JSON
exit 0
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	f := &YTDLP{Command: fake, Timeout: 2 * time.Second, Languages: []string{"zh-Hans", "en"}}

	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !strings.Contains(got.Text, "English fallback.") {
		t.Fatalf("expected fallback transcript, got %+v", got)
	}
}

func TestYTDLPMissingCommandIsOptional(t *testing.T) {
	f := &YTDLP{Command: filepath.Join(t.TempDir(), "missing-ytdlp")}

	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	})

	if err != nil {
		t.Fatalf("missing optional command should not error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil transcript for missing command, got %+v", got)
	}
}
