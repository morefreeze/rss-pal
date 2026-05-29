package ai

import (
	"regexp"
	"unicode/utf8"
)

// imgRE matches markdown image syntax `![alt](url)` and captures the URL.
// Intentionally simple — does not handle parenthesised URLs (rare; tolerable
// to miss). Mirrors the lightweight style of util/imageAlt.flattenImageAltBlankLines
// rather than pulling in a full markdown parser.
var imgRE = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// ExtractImageURLs returns the URLs of `![](...)` markdown images in source
// order. Returns nil (not []string{}) when no images are present, so the
// caller can treat zero-result as "skip vision path" with a simple len()==0.
func ExtractImageURLs(md string) []string {
	matches := imgRE.FindAllStringSubmatch(md, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CountTextRunes returns the rune count of md after stripping all markdown
// image tags. Used to apply the AI_VISION_MAX_TEXT_CHARS heuristic without
// counting alt text or URL chars as "text".
func CountTextRunes(md string) int {
	stripped := imgRE.ReplaceAllString(md, "")
	return utf8.RuneCountInString(stripped)
}
