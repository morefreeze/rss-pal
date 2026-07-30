package youtuberelay

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

var ticketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type mpdDocument struct {
	XMLName                   xml.Name  `xml:"MPD"`
	XMLNS                     string    `xml:"xmlns,attr"`
	Profiles                  string    `xml:"profiles,attr"`
	Type                      string    `xml:"type,attr"`
	MediaPresentationDuration string    `xml:"mediaPresentationDuration,attr"`
	MinBufferTime             string    `xml:"minBufferTime,attr"`
	Period                    mpdPeriod `xml:"Period"`
}

type mpdPeriod struct {
	AdaptationSets []mpdAdaptationSet `xml:"AdaptationSet"`
}

type mpdAdaptationSet struct {
	ID               string            `xml:"id,attr"`
	MimeType         string            `xml:"mimeType,attr"`
	SegmentAlignment string            `xml:"segmentAlignment,attr"`
	StartWithSAP     string            `xml:"startWithSAP,attr"`
	Representation   mpdRepresentation `xml:"Representation"`
}

type mpdRepresentation struct {
	ID                string         `xml:"id,attr"`
	Bandwidth         int64          `xml:"bandwidth,attr"`
	Codecs            string         `xml:"codecs,attr"`
	Width             int            `xml:"width,attr,omitempty"`
	Height            int            `xml:"height,attr,omitempty"`
	FrameRate         string         `xml:"frameRate,attr,omitempty"`
	AudioSamplingRate int            `xml:"audioSamplingRate,attr,omitempty"`
	BaseURL           string         `xml:"BaseURL"`
	SegmentBase       mpdSegmentBase `xml:"SegmentBase"`
}

type mpdSegmentBase struct {
	IndexRange      string            `xml:"indexRange,attr"`
	IndexRangeExact string            `xml:"indexRangeExact,attr"`
	Initialization  mpdInitialization `xml:"Initialization"`
}

type mpdInitialization struct {
	Range string `xml:"range,attr"`
}

func GenerateMPD(
	ticket string,
	durationSeconds float64,
	selection Selection,
	videoRanges MP4IndexRanges,
	audioRanges MP4IndexRanges,
) ([]byte, error) {
	if !ticketPattern.MatchString(ticket) {
		return nil, errors.New("invalid playback ticket")
	}
	if selection.Video == nil || selection.Audio == nil || durationSeconds <= 0 {
		return nil, errors.New("incomplete adaptive selection")
	}
	video := selection.Video
	audio := selection.Audio
	doc := mpdDocument{
		XMLNS:                     "urn:mpeg:dash:schema:mpd:2011",
		Profiles:                  "urn:mpeg:dash:profile:isoff-on-demand:2011",
		Type:                      "static",
		MediaPresentationDuration: dashDuration(durationSeconds),
		MinBufferTime:             "PT2S",
		Period: mpdPeriod{
			AdaptationSets: []mpdAdaptationSet{{
				ID:               "video",
				MimeType:         "video/mp4",
				SegmentAlignment: "true",
				StartWithSAP:     "1",
				Representation: mpdRepresentation{
					ID:        video.ID,
					Bandwidth: bitrateBPS(*video, durationSeconds),
					Codecs:    video.VCodec,
					Width:     video.Width,
					Height:    video.Height,
					FrameRate: trimFloat(video.FPS),
					BaseURL:   fmt.Sprintf("/api/media/youtube/%s/video", ticket),
					SegmentBase: mpdSegmentBase{
						IndexRange:      rangeString(videoRanges.Index),
						IndexRangeExact: "true",
						Initialization:  mpdInitialization{Range: rangeString(videoRanges.Initialization)},
					},
				},
			}, {
				ID:               "audio",
				MimeType:         "audio/mp4",
				SegmentAlignment: "true",
				StartWithSAP:     "1",
				Representation: mpdRepresentation{
					ID:                audio.ID,
					Bandwidth:         bitrateBPS(*audio, durationSeconds),
					Codecs:            audio.ACodec,
					AudioSamplingRate: audio.ASR,
					BaseURL:           fmt.Sprintf("/api/media/youtube/%s/audio", ticket),
					SegmentBase: mpdSegmentBase{
						IndexRange:      rangeString(audioRanges.Index),
						IndexRangeExact: "true",
						Initialization:  mpdInitialization{Range: rangeString(audioRanges.Initialization)},
					},
				},
			}},
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

func dashDuration(seconds float64) string {
	return "PT" + trimFloat(seconds) + "S"
}

func trimFloat(value float64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func rangeString(r ByteRange) string {
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

func bitrateBPS(format Format, duration float64) int64 {
	return int64(math.Round(formatBitrateKbps(format, duration) * 1000))
}
