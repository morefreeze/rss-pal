package rss

import (
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
)

// MediaInfo describes a single audio/video enclosure attached to a feed item.
type MediaInfo struct {
	URL      string
	Type     string
	Duration int // seconds; 0 means unknown
}

// ExtractMedia returns the first audio or video enclosure on item, or nil.
// enclosure.Length is bytes per the RSS spec — not seconds — so we ignore it
// and read item.ITunesExt.Duration when available.
func ExtractMedia(item *gofeed.Item) *MediaInfo {
	if item == nil {
		return nil
	}
	for _, e := range item.Enclosures {
		if e == nil || e.URL == "" {
			continue
		}
		t := strings.ToLower(e.Type)
		if !strings.HasPrefix(t, "audio/") && !strings.HasPrefix(t, "video/") {
			continue
		}
		mi := &MediaInfo{URL: e.URL, Type: e.Type}
		if item.ITunesExt != nil {
			mi.Duration = parseITunesDuration(item.ITunesExt.Duration)
		}
		return mi
	}
	return nil
}

// parseITunesDuration accepts "hh:mm:ss", "mm:ss", or raw integer seconds.
// Returns 0 on any parse failure (including empty input).
func parseITunesDuration(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if !strings.Contains(raw, ":") {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		nums = append(nums, n)
	}
	if len(nums) == 2 {
		return nums[0]*60 + nums[1]
	}
	return nums[0]*3600 + nums[1]*60 + nums[2]
}
