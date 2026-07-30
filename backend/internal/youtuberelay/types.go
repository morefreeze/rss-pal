package youtuberelay

import "errors"

const MaxCombinedKbps = 4000.0

var (
	ErrInvalidVideoID    = errors.New("invalid youtube video id")
	ErrResolveFailed     = errors.New("youtube metadata resolution failed")
	ErrNoCompatibleMedia = errors.New("no compatible youtube media")
)

type Format struct {
	ID             string            `json:"format_id"`
	URL            string            `json:"url"`
	Ext            string            `json:"ext"`
	Protocol       string            `json:"protocol"`
	VCodec         string            `json:"vcodec"`
	ACodec         string            `json:"acodec"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	FPS            float64           `json:"fps"`
	TBR            float64           `json:"tbr"`
	VBR            float64           `json:"vbr"`
	ABR            float64           `json:"abr"`
	ASR            int               `json:"asr"`
	Filesize       int64             `json:"filesize"`
	FilesizeApprox int64             `json:"filesize_approx"`
	HTTPHeaders    map[string]string `json:"http_headers"`
}

type VideoInfo struct {
	ID       string   `json:"id"`
	Duration float64  `json:"duration"`
	Formats  []Format `json:"formats"`
}

type Selection struct {
	Video       *Format
	Audio       *Format
	Progressive *Format
	Quality     int
}

type ResolvedMedia struct {
	Info      VideoInfo
	Selection Selection
}
