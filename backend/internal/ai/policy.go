package ai

import "github.com/bytedance/rss-pal/internal/config"

// ShouldUseVisionAuto reports whether the worker-backfill path should route
// an article through the vision summarizer instead of the plain text path.
// The heuristic is intentionally simple: enough images AND short enough text.
// Image-heavy articles like a sequence of `![](image)` with no body get
// vision; text-heavy articles with the occasional inline image stay on text.
//
// Boundary: text rune count must be STRICTLY LESS THAN MaxTextChars. So
// MaxTextChars=2000 means 1999 runes triggers, 2000 does not. This matches
// the spec table.
func ShouldUseVisionAuto(content string, cfg config.VisionConfig) bool {
	if content == "" {
		return false
	}
	urls := ExtractImageURLs(content)
	if len(urls) < cfg.MinImages {
		return false
	}
	if CountTextRunes(content) >= cfg.MaxTextChars {
		return false
	}
	return true
}
