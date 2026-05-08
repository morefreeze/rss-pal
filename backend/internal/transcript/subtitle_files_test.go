package transcript

import (
	"strings"
	"testing"
)

func TestParseVTT(t *testing.T) {
	in := `WEBVTT

1
00:00:00.500 --> 00:00:02.000
Hello world.

2
00:00:02.000 --> 00:00:04.500
This is a test.

NOTE this is a note line, ignored

3
00:00:04.500 --> 00:00:06.000
Final cue.
`
	got := ParseVTT(in)
	want := "Hello world.\nThis is a test.\nFinal cue."
	if got != want {
		t.Errorf("ParseVTT() =\n  got:  %q\n  want: %q", got, want)
	}
}

func TestParseSRT(t *testing.T) {
	in := "1\n00:00:00,500 --> 00:00:02,000\nHello world.\n\n2\n00:00:02,000 --> 00:00:04,500\nThis is a test.\n"
	got := ParseSRT(in)
	want := "Hello world.\nThis is a test."
	if got != want {
		t.Errorf("ParseSRT() =\n  got:  %q\n  want: %q", got, want)
	}
}

func TestParsePlainText(t *testing.T) {
	in := "  Hello world.\n\n  Goodbye.  \n"
	got := ParsePlainText(in)
	want := "Hello world.\n\nGoodbye."
	if got != want {
		t.Errorf("ParsePlainText() = %q, want %q", got, want)
	}
}

func TestParseSubtitleFile_DispatchesByExtension(t *testing.T) {
	cases := []struct {
		url  string
		body string
		want string
	}{
		{"https://x.com/a.vtt", "WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nHi", "Hi"},
		{"https://x.com/a.srt", "1\n00:00:00,000 --> 00:00:01,000\nHi", "Hi"},
		{"https://x.com/a.txt", "Hi", "Hi"},
		{"https://x.com/a.pdf", "binary garbage", ""},
	}
	for _, tc := range cases {
		got := ParseSubtitleFile(tc.url, tc.body)
		got = strings.TrimSpace(got)
		if got != tc.want {
			t.Errorf("ParseSubtitleFile(%s) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
