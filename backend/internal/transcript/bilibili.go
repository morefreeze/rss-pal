package transcript

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// BilibiliCC fetches captions from Bilibili using the player/v2 API
// (no WBI signing required as of writing).
type BilibiliCC struct {
	ViewURLBase    string // default: "https://api.bilibili.com/x/web-interface/view?bvid="
	PlayerV2URLFmt string // default: "https://api.bilibili.com/x/player/v2?cid=%d&bvid=%s"
	HTTPClient     *http.Client
}

var bvidQueryRe = regexp.MustCompile(`bvid=(BV[A-Za-z0-9]{10})`)

func (f *BilibiliCC) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.MediaType != "video/bilibili" {
		return nil, nil
	}
	bvid := extractBvid(article.MediaURL)
	if bvid == "" {
		return nil, nil
	}
	viewBase := f.ViewURLBase
	if viewBase == "" {
		viewBase = "https://api.bilibili.com/x/web-interface/view?bvid="
	}
	playerFmt := f.PlayerV2URLFmt
	if playerFmt == "" {
		playerFmt = "https://api.bilibili.com/x/player/v2?cid=%d&bvid=%s"
	}
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	body, err := fetchString(ctx, client, viewBase+bvid)
	if err != nil {
		return nil, fmt.Errorf("view: %w", err)
	}
	var view struct {
		Code int `json:"code"`
		Data struct {
			Cid int64 `json:"cid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil || view.Code != 0 || view.Data.Cid == 0 {
		return nil, nil
	}

	body, err = fetchString(ctx, client, fmt.Sprintf(playerFmt, view.Data.Cid, bvid))
	if err != nil {
		return nil, fmt.Errorf("player/v2: %w", err)
	}
	var player struct {
		Code int `json:"code"`
		Data struct {
			Subtitle struct {
				Subtitles []struct {
					Lan         string `json:"lan"`
					SubtitleURL string `json:"subtitle_url"`
				} `json:"subtitles"`
			} `json:"subtitle"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &player); err != nil || player.Code != 0 {
		return nil, nil
	}
	subs := player.Data.Subtitle.Subtitles
	if len(subs) == 0 {
		return nil, nil
	}
	chosen := subs[0]
	for _, s := range subs {
		if strings.HasPrefix(s.Lan, "zh") {
			chosen = s
			break
		}
	}
	subURL := chosen.SubtitleURL
	if strings.HasPrefix(subURL, "//") {
		subURL = "https:" + subURL
	}
	body, err = fetchString(ctx, client, subURL)
	if err != nil {
		return nil, fmt.Errorf("subtitle: %w", err)
	}
	var sub struct {
		Body []struct {
			Content string `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(body), &sub); err != nil || len(sub.Body) == 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, line := range sub.Body {
		s := strings.TrimSpace(line.Content)
		if s == "" {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return nil, nil
	}
	return &Result{Text: text, Source: "Bilibili CC"}, nil
}

func extractBvid(mediaURL string) string {
	m := bvidQueryRe.FindStringSubmatch(mediaURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
