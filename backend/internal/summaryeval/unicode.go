package summaryeval

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject those
// before decoding so file hashes and decoded article inputs remain lossless.
// The JSON decoder still checks syntax; this scan only checks escaped scalars.
func validJSONUnicode(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				continue
			}
			value, ok := hexQuad(raw, i+1)
			if !ok {
				return false
			}
			i += 4
			if value >= 0xdc00 && value <= 0xdfff {
				return false
			}
			if value >= 0xd800 && value <= 0xdbff {
				if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return false
				}
				low, ok := hexQuad(raw, i+3)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

func hexQuad(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, ch := range raw[start : start+4] {
		value <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			value += uint16(ch - '0')
		case ch >= 'a' && ch <= 'f':
			value += uint16(ch - 'a' + 10)
		case ch >= 'A' && ch <= 'F':
			value += uint16(ch - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}
