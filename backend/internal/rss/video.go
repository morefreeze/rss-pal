package rss

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// VideoEmbed describes a recognized embeddable video reference.
type VideoEmbed struct {
	Platform string // "youtube" or "bilibili"
	ID       string // video ID (e.g. "dQw4w9WgXcQ" or "BV1xx411c7mD")
	Start    int    // seconds, 0 if unset
	Page     int    // bilibili-only, 0 means default (page 1)
	EmbedURL string // canonical iframe src
}

// youTubeIDPattern matches the canonical YouTube video ID character set/length.
var youTubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// bilibiliBVPattern matches BV-form Bilibili IDs.
var bilibiliBVPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

// ExtractVideo parses a single URL and returns a VideoEmbed if it matches
// a supported YouTube or Bilibili form. Pure: no network I/O.
func ExtractVideo(rawURL string) (*VideoEmbed, bool) {
	if rawURL == "" {
		return nil, false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")

	if v, ok := extractYouTube(u, host); ok {
		return v, true
	}
	if v, ok := extractBilibili(u, host); ok {
		return v, true
	}
	return nil, false
}

func extractYouTube(u *url.URL, host string) (*VideoEmbed, bool) {
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/embed/"):
			id = strings.TrimPrefix(u.Path, "/embed/")
		case strings.HasPrefix(u.Path, "/shorts/"):
			id = strings.TrimPrefix(u.Path, "/shorts/")
		case strings.HasPrefix(u.Path, "/v/"):
			id = strings.TrimPrefix(u.Path, "/v/")
		}
		// Strip any trailing path segments (e.g. /embed/ID/extra)
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
	default:
		return nil, false
	}
	if !youTubeIDPattern.MatchString(id) {
		return nil, false
	}
	v := &VideoEmbed{Platform: "youtube", ID: id}
	v.Start = parseStartParam(u)
	v.EmbedURL = v.buildEmbedURL()
	return v, true
}

func extractBilibili(u *url.URL, host string) (*VideoEmbed, bool) {
	if host != "bilibili.com" && host != "m.bilibili.com" {
		return nil, false
	}
	if !strings.HasPrefix(u.Path, "/video/") {
		return nil, false
	}
	id := strings.TrimPrefix(u.Path, "/video/")
	id = strings.TrimRight(id, "/")
	if i := strings.Index(id, "/"); i >= 0 {
		id = id[:i]
	}
	if !bilibiliBVPattern.MatchString(id) {
		return nil, false
	}
	v := &VideoEmbed{Platform: "bilibili", ID: id}
	v.Start = parseStartParam(u)
	if p := u.Query().Get("p"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			v.Page = n
		}
	}
	v.EmbedURL = v.buildEmbedURL()
	return v, true
}

// parseStartParam reads ?t= or ?start= or #t= and returns seconds.
// Accepts plain integers ("90"), trailing 's' ("90s"), and "1m30s" form.
func parseStartParam(u *url.URL) int {
	q := u.Query()
	for _, key := range []string{"t", "start"} {
		if v := q.Get(key); v != "" {
			if n := parseDurationSpec(v); n > 0 {
				return n
			}
		}
	}
	if u.Fragment != "" && strings.HasPrefix(u.Fragment, "t=") {
		if n := parseDurationSpec(strings.TrimPrefix(u.Fragment, "t=")); n > 0 {
			return n
		}
	}
	return 0
}

// parseDurationSpec accepts "90", "90s", "1m30s", "1h2m3s".
func parseDurationSpec(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	// 1h2m3s form
	var total, cur int
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
		case c == 'h':
			total += cur * 3600
			cur = 0
		case c == 'm':
			total += cur * 60
			cur = 0
		case c == 's':
			total += cur
			cur = 0
		default:
			return 0
		}
	}
	return total + cur
}

// Placeholder returns the markdown-safe inline placeholder used to represent
// this embed inside article content. Form: [[video:<platform>:<id>(?query)?]]
// where the optional query carries `page` (bilibili) and `start` keys.
func (v *VideoEmbed) Placeholder() string {
	if v == nil || v.Platform == "" || v.ID == "" {
		return ""
	}
	var params []string
	if v.Platform == "bilibili" && v.Page > 0 {
		params = append(params, "page="+strconv.Itoa(v.Page))
	}
	if v.Start > 0 {
		params = append(params, "start="+strconv.Itoa(v.Start))
	}
	if len(params) == 0 {
		return "[[video:" + v.Platform + ":" + v.ID + "]]"
	}
	return "[[video:" + v.Platform + ":" + v.ID + "?" + strings.Join(params, "&") + "]]"
}

// ParseEmbedURL is the inverse of buildEmbedURL: given a stored embed URL
// (as written into media_url), return the VideoEmbed components.
func ParseEmbedURL(rawURL string) (*VideoEmbed, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	switch host {
	case "youtube-nocookie.com", "youtube.com":
		if !strings.HasPrefix(u.Path, "/embed/") {
			return nil, false
		}
		id := strings.TrimPrefix(u.Path, "/embed/")
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
		if !youTubeIDPattern.MatchString(id) {
			return nil, false
		}
		v := &VideoEmbed{Platform: "youtube", ID: id, EmbedURL: rawURL}
		if s := u.Query().Get("start"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				v.Start = n
			}
		}
		return v, true
	case "player.bilibili.com":
		id := u.Query().Get("bvid")
		if !bilibiliBVPattern.MatchString(id) {
			return nil, false
		}
		v := &VideoEmbed{Platform: "bilibili", ID: id, EmbedURL: rawURL}
		if p := u.Query().Get("page"); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				v.Page = n
			}
		}
		if s := u.Query().Get("t"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				v.Start = n
			}
		}
		return v, true
	}
	return nil, false
}

// ExtractVideoMedia is a convenience wrapper that returns a *MediaInfo
// when rawURL is a recognized video. Returns nil otherwise. Callers that
// already work in terms of MediaInfo (worker, feed/content APIs) can fall
// back to ExtractMedia(item) when this returns nil. Video wins over audio
// because video-bearing feeds occasionally also carry an audio enclosure
// (rare; we choose visibility over completeness).
func ExtractVideoMedia(rawURL string) *MediaInfo {
	v, ok := ExtractVideo(rawURL)
	if !ok {
		return nil
	}
	return &MediaInfo{
		URL:  v.EmbedURL,
		Type: "video/" + v.Platform,
	}
}

// buildEmbedURL constructs the canonical iframe src for the embed.
func (v *VideoEmbed) buildEmbedURL() string {
	switch v.Platform {
	case "youtube":
		s := "https://www.youtube-nocookie.com/embed/" + v.ID + "?rel=0"
		if v.Start > 0 {
			s += "&start=" + strconv.Itoa(v.Start)
		}
		return s
	case "bilibili":
		page := v.Page
		if page <= 0 {
			page = 1
		}
		s := "https://player.bilibili.com/player.html?bvid=" + v.ID +
			"&high_quality=1&autoplay=0&page=" + strconv.Itoa(page)
		if v.Start > 0 {
			s += "&t=" + strconv.Itoa(v.Start)
		}
		return s
	}
	return ""
}
