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
