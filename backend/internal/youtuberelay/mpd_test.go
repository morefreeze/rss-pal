package youtuberelay

import (
	"strings"
	"testing"
)

const testTicket = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestGenerateMPDUsesOnlyTicketedSameOriginURLs(t *testing.T) {
	selection := Selection{
		Video: &Format{
			ID: "137", URL: googleURL("video"), Ext: "mp4",
			VCodec: "avc1.640028", ACodec: "none",
			Width: 1920, Height: 1080, FPS: 30, TBR: 3650,
		},
		Audio: &Format{
			ID: "140", URL: googleURL("audio"), Ext: "m4a",
			VCodec: "none", ACodec: "mp4a.40.2", ASR: 44100, ABR: 128,
		},
		Quality: 1080,
	}

	xmlBytes, err := GenerateMPD(
		testTicket,
		212,
		selection,
		MP4IndexRanges{
			Initialization: ByteRange{Start: 0, End: 103},
			Index:          ByteRange{Start: 104, End: 151},
		},
		MP4IndexRanges{
			Initialization: ByteRange{Start: 0, End: 79},
			Index:          ByteRange{Start: 80, End: 119},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	xmlText := string(xmlBytes)
	for _, want := range []string{
		`type="static"`,
		`mediaPresentationDuration="PT212S"`,
		`mimeType="video/mp4"`,
		`codecs="avc1.640028"`,
		`width="1920"`,
		`height="1080"`,
		`frameRate="30"`,
		`indexRange="104-151"`,
		`range="0-103"`,
		`mimeType="audio/mp4"`,
		`codecs="mp4a.40.2"`,
		`audioSamplingRate="44100"`,
		`indexRange="80-119"`,
		`<BaseURL>/api/media/youtube/` + testTicket + `/video</BaseURL>`,
		`<BaseURL>/api/media/youtube/` + testTicket + `/audio</BaseURL>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("MPD missing %q:\n%s", want, xmlText)
		}
	}
	if strings.Contains(xmlText, "googlevideo.com") {
		t.Fatalf("MPD leaked upstream URL:\n%s", xmlText)
	}
}

func TestGenerateMPDRejectsUnsafeTicket(t *testing.T) {
	selection := Selection{
		Video: &Format{ID: "137", VCodec: "avc1.640028", Width: 1920, Height: 1080, FPS: 30, TBR: 3000},
		Audio: &Format{ID: "140", ACodec: "mp4a.40.2", ASR: 44100, ABR: 128},
	}
	ranges := MP4IndexRanges{
		Initialization: ByteRange{Start: 0, End: 10},
		Index:          ByteRange{Start: 11, End: 20},
	}

	if _, err := GenerateMPD("../unsafe", 100, selection, ranges, ranges); err == nil {
		t.Fatal("expected unsafe ticket error")
	}
}
