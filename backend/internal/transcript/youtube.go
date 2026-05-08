package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// YouTubeCC fetches captions from a YouTube watch page by parsing
// ytInitialPlayerResponse and following the chosen caption track's
// baseUrl with fmt=json3.
type YouTubeCC struct {
	// WatchURLBase is the prefix used to build the watch URL. Defaults to
	// "https://www.youtube.com/watch?v=" when empty. Override in tests.
	WatchURLBase string

	// HTTPClient lets tests override timeouts. Defaults to a 30s client.
	HTTPClient *http.Client
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// embedIDRe matches the 11-char video ID inside a youtube-nocookie embed URL.
var embedIDRe = regexp.MustCompile(`/embed/([A-Za-z0-9_-]{11})`)

// playerResponseRe locates the start of ytInitialPlayerResponse so we can
// then do a balanced-brace scan to extract the JSON object.
var playerResponseRe = regexp.MustCompile(`(?s)ytInitialPlayerResponse\s*=\s*(\{)`)

func (f *YouTubeCC) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.MediaType != "video/youtube" {
		return nil, nil
	}
	id := extractYouTubeID(article.MediaURL)
	if id == "" {
		return nil, nil
	}
	base := f.WatchURLBase
	if base == "" {
		base = "https://www.youtube.com/watch?v="
	}
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	html, err := fetchString(ctx, client, base+id)
	if err != nil {
		return nil, fmt.Errorf("fetch watch page: %w", err)
	}
	tracks, err := extractCaptionTracks(html)
	if err != nil {
		return nil, nil // no ytInitialPlayerResponse → no transcript
	}
	if len(tracks) == 0 {
		return nil, nil
	}
	chosen := pickTrack(tracks)
	trackURL := chosen.BaseURL
	if !strings.Contains(trackURL, "fmt=") {
		sep := "?"
		if strings.Contains(trackURL, "?") {
			sep = "&"
		}
		trackURL = trackURL + sep + "fmt=json3"
	}

	body, err := fetchString(ctx, client, trackURL)
	if err != nil {
		return nil, fmt.Errorf("fetch track: %w", err)
	}
	text, err := parseJSON3(body)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	return &Result{Text: text, Source: sourceLabel(chosen)}, nil
}

func extractYouTubeID(mediaURL string) string {
	m := embedIDRe.FindStringSubmatch(mediaURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"`
}

func extractCaptionTracks(html string) ([]captionTrack, error) {
	loc := playerResponseRe.FindStringIndex(html)
	if loc == nil {
		return nil, errors.New("ytInitialPlayerResponse not found")
	}
	start := loc[1] - 1
	end := scanBalancedJSON(html, start)
	if end < 0 {
		return nil, errors.New("could not scan balanced JSON")
	}
	var doc struct {
		Captions struct {
			PlayerCaptionsTracklistRenderer struct {
				CaptionTracks []captionTrack `json:"captionTracks"`
			} `json:"playerCaptionsTracklistRenderer"`
		} `json:"captions"`
	}
	if err := json.Unmarshal([]byte(html[start:end]), &doc); err != nil {
		return nil, err
	}
	return doc.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks, nil
}

func scanBalancedJSON(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '{' {
		return -1
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func pickTrack(tracks []captionTrack) captionTrack {
	prefs := []string{"zh", "en"}
	for _, p := range prefs {
		for _, t := range tracks {
			if strings.HasPrefix(t.LanguageCode, p) && t.Kind != "asr" {
				return t
			}
		}
		for _, t := range tracks {
			if strings.HasPrefix(t.LanguageCode, p) {
				return t
			}
		}
	}
	for _, t := range tracks {
		if t.Kind != "asr" {
			return t
		}
	}
	return tracks[0]
}

func sourceLabel(t captionTrack) string {
	if t.Kind == "asr" {
		return "YouTube 自动字幕"
	}
	return "YouTube CC"
}

type json3Doc struct {
	Events []struct {
		Segs []struct {
			Utf8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func parseJSON3(body string) (string, error) {
	var doc json3Doc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, ev := range doc.Events {
		var line strings.Builder
		for _, seg := range ev.Segs {
			line.WriteString(seg.Utf8)
		}
		s := strings.TrimSpace(line.String())
		if s == "" {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func fetchString(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
