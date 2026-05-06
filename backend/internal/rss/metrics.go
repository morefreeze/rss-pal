package rss

import (
	"math"
	"strings"
	"unicode"
)

// ComputeMetrics returns word_count and reading_minutes for an article body.
// Heuristic:
//   - count Han characters (CJK Unified Ideographs) as 1 each
//   - strip Han, then count whitespace-separated tokens as English words
//   - reading speed: 300 zh chars/min, 250 en words/min
//   - reading_minutes = max(1, round(zh/300 + en/250)) when word_count > 0
//
// HTML tags are stripped before counting so the same input as the AI summarizer
// (raw HTML) produces sensible numbers.
func ComputeMetrics(content string) (wordCount, readingMinutes int) {
	text := stripHTMLForMetrics(content)
	if text == "" {
		return 0, 0
	}

	zhChars := 0
	var nonHan strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			zhChars++
		} else {
			nonHan.WriteRune(r)
		}
	}

	enWords := 0
	for _, w := range strings.Fields(nonHan.String()) {
		if w != "" {
			enWords++
		}
	}

	wordCount = zhChars + enWords
	if wordCount == 0 {
		return 0, 0
	}

	mins := float64(zhChars)/300.0 + float64(enWords)/250.0
	readingMinutes = int(math.Round(mins))
	if readingMinutes < 1 {
		readingMinutes = 1
	}
	return wordCount, readingMinutes
}

// stripHTMLForMetrics removes tags and collapses whitespace.
// We do not try to be perfect — this is just for character counting.
func stripHTMLForMetrics(s string) string {
	out := make([]rune, 0, len(s))
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out = append(out, r)
		}
	}
	return strings.TrimSpace(string(out))
}
