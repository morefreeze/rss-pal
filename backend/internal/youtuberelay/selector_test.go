package youtuberelay

import (
	"fmt"
	"testing"
)

func googleURL(id string) string {
	return fmt.Sprintf("https://rr1---sn-test.googlevideo.com/videoplayback?id=%s", id)
}

func TestSelectFormatsPrefers1080UnderCap(t *testing.T) {
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 212, Formats: []Format{
		{ID: "137", URL: googleURL("v137"), Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 3650},
		{ID: "399", URL: googleURL("v399"), Ext: "mp4", VCodec: "av01.0.08M.08", ACodec: "none", Height: 1080, FPS: 30, TBR: 2500},
		{ID: "136", URL: googleURL("v136"), Ext: "mp4", VCodec: "avc1.4d401f", ACodec: "none", Height: 720, FPS: 30, TBR: 2200},
		{ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128, TBR: 128},
		{ID: "22", URL: googleURL("p22"), Ext: "mp4", VCodec: "avc1.64001F", ACodec: "mp4a.40.2", Height: 720, FPS: 30, TBR: 2400},
	}}

	got, err := SelectFormats(info, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Video == nil || got.Video.ID != "137" {
		t.Fatalf("video = %+v, want format 137", got.Video)
	}
	if got.Audio == nil || got.Audio.ID != "140" {
		t.Fatalf("audio = %+v, want format 140", got.Audio)
	}
	if got.Quality != 1080 {
		t.Fatalf("quality = %d, want 1080", got.Quality)
	}
	if got.Progressive == nil || got.Progressive.ID != "22" {
		t.Fatalf("progressive = %+v, want format 22", got.Progressive)
	}
}

func TestSelectFormatsFallsBackTo720When1080ExceedsCap(t *testing.T) {
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 212, Formats: []Format{
		{ID: "137", URL: googleURL("v137"), Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 4300},
		{ID: "136", URL: googleURL("v136"), Ext: "mp4", VCodec: "avc1.4d401f", ACodec: "none", Height: 720, FPS: 30, TBR: 2200},
		{ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128},
	}}

	got, err := SelectFormats(info, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Video == nil || got.Video.ID != "136" || got.Quality != 720 {
		t.Fatalf("selection = %+v, want 720p format 136", got)
	}
}

func TestSelectFormatsRejectsUnsafeAndIncompatibleFormats(t *testing.T) {
	cases := []struct {
		name   string
		format Format
	}{
		{
			name:   "unsafe host",
			format: Format{ID: "137", URL: "https://example.com/video.mp4", Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 2000},
		},
		{
			name:   "av1",
			format: Format{ID: "399", URL: googleURL("v399"), Ext: "mp4", VCodec: "av01.0.08M.08", ACodec: "none", Height: 1080, FPS: 30, TBR: 2000},
		},
		{
			name:   "sixty fps",
			format: Format{ID: "299", URL: googleURL("v299"), Ext: "mp4", VCodec: "avc1.64002a", ACodec: "none", Height: 1080, FPS: 60, TBR: 2000},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 100, Formats: []Format{
				tc.format,
				{ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128},
			}}
			if _, err := SelectFormats(info, 4000); err != ErrNoCompatibleMedia {
				t.Fatalf("error = %v, want ErrNoCompatibleMedia", err)
			}
		})
	}
}

func TestSelectFormatsEstimatesBitrateFromFileSize(t *testing.T) {
	info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 100, Formats: []Format{
		{ID: "137", URL: googleURL("v137"), Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, Filesize: 50_000_000},
		{ID: "136", URL: googleURL("v136"), Ext: "mp4", VCodec: "avc1.4d401f", ACodec: "none", Height: 720, FPS: 30, FilesizeApprox: 20_000_000},
		{ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", Filesize: 1_600_000},
	}}

	got, err := SelectFormats(info, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Video == nil || got.Video.ID != "136" {
		t.Fatalf("video = %+v, want filesize-estimated 720p format", got.Video)
	}
}
