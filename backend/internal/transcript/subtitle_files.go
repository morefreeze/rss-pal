package transcript

import (
	"regexp"
	"strings"
)

// Timestamp lines look like: 00:00:00.500 --> 00:00:02.000  (VTT)
// or:                       00:00:00,500 --> 00:00:02,000  (SRT)
var timestampRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}[,.]\d{3}\s+-->\s+\d{2}:\d{2}:\d{2}[,.]\d{3}`)
var cueIndexRe = regexp.MustCompile(`^\d+$`)

// ParseVTT extracts text-only content from a WebVTT body.
// Header line, NOTE blocks, cue indices, and timestamp lines are dropped.
func ParseVTT(body string) string {
	var b strings.Builder
	skipBlock := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			skipBlock = false
			continue
		}
		if strings.HasPrefix(trimmed, "WEBVTT") {
			continue
		}
		if strings.HasPrefix(trimmed, "NOTE") {
			skipBlock = true
			continue
		}
		if skipBlock {
			continue
		}
		if cueIndexRe.MatchString(trimmed) {
			continue
		}
		if timestampRe.MatchString(trimmed) {
			continue
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ParseSRT extracts text-only content from a SubRip body.
func ParseSRT(body string) string {
	var b strings.Builder
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if cueIndexRe.MatchString(trimmed) {
			continue
		}
		if timestampRe.MatchString(trimmed) {
			continue
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ParsePlainText collapses excessive whitespace but preserves paragraph breaks.
func ParsePlainText(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ParseSubtitleFile dispatches by URL suffix. Unsupported extensions
// return empty string.
func ParseSubtitleFile(rawURL, body string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasSuffix(lower, ".vtt"):
		return ParseVTT(body)
	case strings.HasSuffix(lower, ".srt"):
		return ParseSRT(body)
	case strings.HasSuffix(lower, ".txt"):
		return ParsePlainText(body)
	default:
		return ""
	}
}
