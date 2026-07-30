package youtuberelay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func mp4Box(kind string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(out)))
	copy(out[4:8], kind)
	copy(out[8:], payload)
	return out
}

func extendedMP4Box(kind string, payload []byte) []byte {
	out := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint32(out[:4], 1)
	copy(out[4:8], kind)
	binary.BigEndian.PutUint64(out[8:16], uint64(len(out)))
	copy(out[16:], payload)
	return out
}

func TestParseMP4IndexRanges(t *testing.T) {
	prefix := bytes.Join([][]byte{
		mp4Box("ftyp", make([]byte, 16)),
		mp4Box("moov", make([]byte, 72)),
		mp4Box("sidx", make([]byte, 40)),
		mp4Box("moof", make([]byte, 24)),
	}, nil)

	got, err := ParseMP4IndexRanges(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if got.Initialization != (ByteRange{Start: 0, End: 103}) {
		t.Fatalf("initialization = %+v, want 0-103", got.Initialization)
	}
	if got.Index != (ByteRange{Start: 104, End: 151}) {
		t.Fatalf("index = %+v, want 104-151", got.Index)
	}
}

func TestParseMP4IndexRangesSupportsExtendedSize(t *testing.T) {
	prefix := bytes.Join([][]byte{
		mp4Box("ftyp", make([]byte, 8)),
		extendedMP4Box("moov", make([]byte, 40)),
		extendedMP4Box("sidx", make([]byte, 24)),
	}, nil)

	got, err := ParseMP4IndexRanges(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if got.Initialization.End != 71 || got.Index != (ByteRange{Start: 72, End: 111}) {
		t.Fatalf("unexpected ranges: %+v", got)
	}
}

func TestParseMP4IndexRangesReportsIncompleteBox(t *testing.T) {
	prefix := append(mp4Box("ftyp", make([]byte, 8)), mp4Box("moov", make([]byte, 40))...)
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[:4], 80)
	copy(header[4:], "sidx")
	prefix = append(prefix, header...)

	_, err := ParseMP4IndexRanges(prefix)
	if !errors.Is(err, ErrMP4Incomplete) {
		t.Fatalf("error = %v, want ErrMP4Incomplete", err)
	}
}

func TestParseMP4IndexRangesRejectsMalformedOrMissingBoxes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{name: "size smaller than header", data: []byte{0, 0, 0, 4, 'f', 't', 'y', 'p'}},
		{name: "size zero", data: []byte{0, 0, 0, 0, 'f', 't', 'y', 'p'}},
		{name: "missing ftyp", data: append(mp4Box("moov", nil), mp4Box("sidx", nil)...)},
		{name: "missing moov", data: append(mp4Box("ftyp", nil), mp4Box("sidx", nil)...)},
		{name: "missing sidx", data: append(mp4Box("ftyp", nil), mp4Box("moov", nil)...)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseMP4IndexRanges(tc.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
