package transcript

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/rss"
)

// YTDLP fetches subtitles via the yt-dlp executable. It is intentionally a
// fallback behind the pure-HTTP platform strategies because yt-dlp pulls in a
// larger runtime dependency, but it tracks platform-side subtitle changes much
// better than our small scrapers can.
type YTDLP struct {
	Command   string
	Timeout   time.Duration
	Languages []string
}

func (f *YTDLP) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || !ytdlpSupportedMedia(article.MediaType) {
		return nil, nil
	}
	sourceURL := ytdlpSourceURL(article)
	if sourceURL == "" {
		return nil, nil
	}

	command := f.Command
	if command == "" {
		command = "yt-dlp"
	}
	commandPath, err := exec.LookPath(command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, nil
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tmp, err := os.MkdirTemp("", "rss-pal-ytdlp-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	var firstErr error
	for i, lang := range f.languages() {
		attemptDir := filepath.Join(tmp, fmt.Sprintf("attempt-%02d", i))
		if err := os.Mkdir(attemptDir, 0o755); err != nil {
			return nil, err
		}
		runErr, output := runYTDLP(cmdCtx, commandPath, attemptDir, lang, sourceURL)
		result, parseErr := f.firstSubtitleResult(attemptDir)
		if parseErr != nil {
			return nil, parseErr
		}
		if result != nil {
			return result, nil
		}
		if cmdCtx.Err() != nil {
			return nil, cmdCtx.Err()
		}
		if runErr != nil && !ytdlpOutputSaysNoSubtitles(output) && firstErr == nil {
			firstErr = fmt.Errorf("yt-dlp: %w: %s", runErr, tailString(output, 2000))
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

func runYTDLP(ctx context.Context, commandPath, dir, lang, sourceURL string) (error, string) {
	args := []string{
		"--no-update",
		"--no-warnings",
		"--no-playlist",
		"--skip-download",
		"--write-subs",
		"--write-auto-subs",
		"--sub-langs", lang,
		"--sub-format", "json3/vtt/srt/best",
		"--output", filepath.Join(dir, "sub.%(ext)s"),
		sourceURL,
	}
	cmd := exec.CommandContext(ctx, commandPath, args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	return cmd.Run(), output.String()
}

func (f *YTDLP) languages() []string {
	if len(f.Languages) > 0 {
		return f.Languages
	}
	return []string{"zh-Hans", "zh-Hant", "zh-CN", "zh", "en-orig", "en"}
}

func ytdlpSupportedMedia(mediaType string) bool {
	return mediaType == "video/youtube" || mediaType == "video/bilibili"
}

func ytdlpSourceURL(article *model.Article) string {
	if article.URL != "" {
		return article.URL
	}
	for _, raw := range []string{article.MediaURL} {
		v, ok := rss.ExtractVideo(raw)
		if !ok {
			continue
		}
		switch v.Platform {
		case "youtube":
			return "https://www.youtube.com/watch?v=" + v.ID
		case "bilibili":
			return "https://www.bilibili.com/video/" + v.ID
		}
	}
	return ""
}

type ytdlpSubtitleFile struct {
	path string
	lang string
	rank int
	name string
}

func (f *YTDLP) firstSubtitleResult(dir string) (*Result, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var subs []ytdlpSubtitleFile
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json3" && ext != ".vtt" && ext != ".srt" {
			continue
		}
		subs = append(subs, ytdlpSubtitleFile{
			path: filepath.Join(dir, name),
			lang: ytdlpSubtitleLang(name),
			rank: ytdlpLanguageRank(ytdlpSubtitleLang(name), f.languages()),
			name: name,
		})
	}
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].rank != subs[j].rank {
			return subs[i].rank < subs[j].rank
		}
		return subs[i].name < subs[j].name
	})
	for _, sub := range subs {
		body, err := os.ReadFile(sub.path)
		if err != nil {
			return nil, err
		}
		text := parseYTDLPSubtitleFile(sub.path, string(body))
		if strings.TrimSpace(text) == "" {
			continue
		}
		return &Result{Text: text, Source: "yt-dlp 字幕"}, nil
	}
	return nil, nil
}

func ytdlpSubtitleLang(name string) string {
	base := name
	for _, ext := range []string{".json3", ".vtt", ".srt"} {
		base = strings.TrimSuffix(base, ext)
	}
	if strings.HasPrefix(base, "sub.") {
		return strings.TrimPrefix(base, "sub.")
	}
	parts := strings.Split(base, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

func ytdlpLanguageRank(lang string, prefs []string) int {
	for i, p := range prefs {
		if lang == p {
			return i
		}
	}
	for i, p := range prefs {
		if p != "" && strings.HasPrefix(lang, p+"-") {
			return i
		}
	}
	return len(prefs) + 1
}

func parseYTDLPSubtitleFile(path, body string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json3":
		text, err := parseJSON3(body)
		if err == nil {
			return text
		}
	}
	return ParseSubtitleFile(path, body)
}

func ytdlpOutputSaysNoSubtitles(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "has no subtitles") ||
		strings.Contains(lower, "no subtitles") ||
		strings.Contains(lower, "there are no subtitles")
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[len(s)-max:])
}
