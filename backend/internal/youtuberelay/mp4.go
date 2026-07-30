package youtuberelay

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var ErrMP4Incomplete = errors.New("incomplete mp4 box")

type ByteRange struct {
	Start int64
	End   int64
}

type MP4IndexRanges struct {
	Initialization ByteRange
	Index          ByteRange
}

func ParseMP4IndexRanges(prefix []byte) (MP4IndexRanges, error) {
	var (
		offset   int64
		foundFTY bool
		moovEnd  int64 = -1
		sidx     ByteRange
		foundSID bool
	)

	for boxes := 0; offset < int64(len(prefix)); boxes++ {
		if boxes >= 64 {
			return MP4IndexRanges{}, errors.New("too many top-level mp4 boxes")
		}
		if int64(len(prefix))-offset < 8 {
			return MP4IndexRanges{}, ErrMP4Incomplete
		}
		start := offset
		size := int64(binary.BigEndian.Uint32(prefix[offset : offset+4]))
		kind := string(prefix[offset+4 : offset+8])
		headerSize := int64(8)
		if size == 1 {
			if int64(len(prefix))-offset < 16 {
				return MP4IndexRanges{}, ErrMP4Incomplete
			}
			size64 := binary.BigEndian.Uint64(prefix[offset+8 : offset+16])
			if size64 > uint64(^uint64(0)>>1) {
				return MP4IndexRanges{}, errors.New("mp4 box size overflows int64")
			}
			size = int64(size64)
			headerSize = 16
		}
		if size == 0 {
			return MP4IndexRanges{}, errors.New("unbounded top-level mp4 box")
		}
		if size < headerSize {
			return MP4IndexRanges{}, fmt.Errorf("invalid %s box size %d", kind, size)
		}
		if size > int64(len(prefix))-offset {
			return MP4IndexRanges{}, ErrMP4Incomplete
		}
		end := start + size - 1
		switch kind {
		case "ftyp":
			foundFTY = true
		case "moov":
			moovEnd = end
		case "sidx":
			sidx = ByteRange{Start: start, End: end}
			foundSID = true
		}
		offset += size
		if foundFTY && moovEnd >= 0 && foundSID {
			return MP4IndexRanges{
				Initialization: ByteRange{Start: 0, End: moovEnd},
				Index:          sidx,
			}, nil
		}
	}

	switch {
	case !foundFTY:
		return MP4IndexRanges{}, errors.New("mp4 ftyp box not found")
	case moovEnd < 0:
		return MP4IndexRanges{}, errors.New("mp4 moov box not found")
	case !foundSID:
		return MP4IndexRanges{}, errors.New("mp4 sidx box not found")
	default:
		return MP4IndexRanges{}, errors.New("invalid mp4 index layout")
	}
}
