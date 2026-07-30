package youtuberelay

import (
	"net/url"
	"sort"
	"strings"
)

type formatPair struct {
	video Format
	audio Format
	kbps  float64
}

func SelectFormats(info VideoInfo, maxCombinedKbps float64) (Selection, error) {
	if maxCombinedKbps <= 0 {
		maxCombinedKbps = MaxCombinedKbps
	}

	var videos []Format
	var audios []Format
	var progressive []Format
	for _, format := range info.Formats {
		if !safeMediaURL(format.URL) {
			continue
		}
		switch {
		case adaptiveVideoCompatible(format):
			videos = append(videos, format)
		case adaptiveAudioCompatible(format):
			audios = append(audios, format)
		case progressiveCompatible(format):
			progressive = append(progressive, format)
		}
	}

	var pairs []formatPair
	for _, video := range videos {
		videoRate := formatBitrateKbps(video, info.Duration)
		if videoRate <= 0 {
			continue
		}
		for _, audio := range audios {
			audioRate := formatBitrateKbps(audio, info.Duration)
			if audioRate <= 0 {
				continue
			}
			total := videoRate + audioRate
			if total <= maxCombinedKbps {
				pairs = append(pairs, formatPair{video: video, audio: audio, kbps: total})
			}
		}
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].video.Height != pairs[j].video.Height {
			return pairs[i].video.Height > pairs[j].video.Height
		}
		return pairs[i].kbps > pairs[j].kbps
	})
	sort.SliceStable(progressive, func(i, j int) bool {
		if progressive[i].Height != progressive[j].Height {
			return progressive[i].Height > progressive[j].Height
		}
		return formatBitrateKbps(progressive[i], info.Duration) >
			formatBitrateKbps(progressive[j], info.Duration)
	})

	var selected Selection
	if len(progressive) > 0 {
		format := progressive[0]
		selected.Progressive = &format
	}
	if len(pairs) == 0 {
		if selected.Progressive != nil {
			selected.Quality = selected.Progressive.Height
			return selected, nil
		}
		return Selection{}, ErrNoCompatibleMedia
	}

	video := pairs[0].video
	audio := pairs[0].audio
	selected.Video = &video
	selected.Audio = &audio
	selected.Quality = video.Height
	return selected, nil
}

func safeMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "googlevideo.com" || strings.HasSuffix(host, ".googlevideo.com")
}

func adaptiveVideoCompatible(format Format) bool {
	return format.Ext == "mp4" &&
		strings.HasPrefix(strings.ToLower(format.VCodec), "avc1") &&
		format.ACodec == "none" &&
		format.Height >= 720 && format.Height <= 1080 &&
		(format.FPS == 0 || format.FPS <= 30)
}

func adaptiveAudioCompatible(format Format) bool {
	ext := strings.ToLower(format.Ext)
	return (ext == "m4a" || ext == "mp4") &&
		format.VCodec == "none" &&
		strings.HasPrefix(strings.ToLower(format.ACodec), "mp4a")
}

func progressiveCompatible(format Format) bool {
	return strings.EqualFold(format.Ext, "mp4") &&
		strings.HasPrefix(strings.ToLower(format.VCodec), "avc1") &&
		strings.HasPrefix(strings.ToLower(format.ACodec), "mp4a") &&
		format.Height > 0 && format.Height <= 720 &&
		(format.FPS == 0 || format.FPS <= 30)
}

func formatBitrateKbps(format Format, durationSeconds float64) float64 {
	if format.TBR > 0 {
		return format.TBR
	}
	if format.VCodec == "none" && format.ABR > 0 {
		return format.ABR
	}
	if format.ACodec == "none" && format.VBR > 0 {
		return format.VBR
	}
	size := format.Filesize
	if size <= 0 {
		size = format.FilesizeApprox
	}
	if size <= 0 || durationSeconds <= 0 {
		return 0
	}
	return float64(size) * 8 / durationSeconds / 1000
}
