package rss

import (
	"regexp"
	"strings"
)

// orderedListLineRe matches "N. " at line start. Used to insert a blank
// line before list items that follow non-list content, otherwise CommonMark
// parsers treat them as continuation text and skip the <ol> rendering.
var orderedListLineRe = regexp.MustCompile(`^\d+\.\s`)

// normalizeOrderedListBreaks inserts a blank line before any ordered-list
// item that's preceded by a non-empty, non-list line. Tweet writers rarely
// add the blank line themselves; without it react-markdown + remark-gfm
// merge the "1. ..." into the prior paragraph instead of rendering an <ol>
// (no list indent, no marker spacing — looks misaligned in the reader).
func normalizeOrderedListBreaks(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines)+4)
	for i, line := range lines {
		if orderedListLineRe.MatchString(line) && i > 0 {
			prev := strings.TrimSpace(lines[i-1])
			if prev != "" && !orderedListLineRe.MatchString(prev) {
				out = append(out, "")
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// BuildTweetContent renders a TweetCapture as the article body. The first
// line is a markdown blockquote that carries the author byline and date,
// which the reader renders just like Twitter's own attribution row. Empty
// fields are silently dropped — image-only and quote-only tweets still
// produce a useful article body.
//
// This is the canonical formatter shared by:
//   - bookmarklet capture (single tweet pushed by user)
//   - extension ingest (batched tweets from list/user/bookmarks)
func BuildTweetContent(cap *TweetCapture) string {
	var sections []string

	if byline := BuildTweetByline(cap); byline != "" {
		sections = append(sections, byline)
	}
	if cap.ArticleTitle != "" {
		sections = append(sections, "# "+cap.ArticleTitle)
	}
	if cap.TextMarkdown != "" {
		sections = append(sections, normalizeOrderedListBreaks(cap.TextMarkdown))
	}
	for _, img := range cap.ImageURLs {
		sections = append(sections, "![]("+img+")")
	}
	if quote := buildQuoteSection(cap.Quote); quote != "" {
		sections = append(sections, quote)
	}

	return strings.Join(sections, "\n\n")
}

// buildQuoteSection renders a Quote as a markdown block: an "引用" header
// line linking to the source (with @handle (DisplayName) · YYYY-MM-DD when
// available), followed by a blockquote of the excerpt. Degrades gracefully:
// a quote with only a URL falls back to the bare "引用: <url>" line so we
// don't lose the link.
func buildQuoteSection(q *Quote) string {
	if q == nil || q.URL == "" {
		return ""
	}
	if q.Author == "" && q.Excerpt == "" && len(q.ImageURLs) == 0 {
		return "引用: " + q.URL
	}

	var b strings.Builder
	b.WriteString("**引用** ")
	if q.Author != "" {
		b.WriteString("[@")
		b.WriteString(q.Author)
		if q.DisplayName != "" {
			b.WriteString(" (")
			b.WriteString(q.DisplayName)
			b.WriteString(")")
		}
		b.WriteString("](")
		b.WriteString(q.URL)
		b.WriteString(")")
	} else {
		b.WriteString("[")
		b.WriteString(q.URL)
		b.WriteString("](")
		b.WriteString(q.URL)
		b.WriteString(")")
	}
	if !q.PublishedAt.IsZero() {
		b.WriteString(" · ")
		b.WriteString(q.PublishedAt.UTC().Format("2006-01-02"))
	}

	if q.Excerpt != "" {
		b.WriteString("\n\n")
		for i, line := range strings.Split(q.Excerpt, "\n") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("> ")
			b.WriteString(line)
		}
	}
	for _, img := range q.ImageURLs {
		b.WriteString("\n\n> ![](")
		b.WriteString(img)
		b.WriteString(")")
	}
	return b.String()
}

// BuildTweetByline produces "> @handle (DisplayName) · YYYY-MM-DD" when
// possible, degrading gracefully when any component is missing. Returns ""
// only when even the handle is missing (extremely rare — we got the
// statusID from a URL that also contained the handle, so this would mean
// the parser couldn't find any profile anchor inside the focal article).
//
// Frontend uses this to parse author info back out of stored content.
func BuildTweetByline(cap *TweetCapture) string {
	if cap.Author == "" && cap.DisplayName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("> ")
	if cap.Author != "" {
		b.WriteString("@")
		b.WriteString(cap.Author)
	}
	if cap.DisplayName != "" {
		if cap.Author != "" {
			b.WriteString(" ")
		}
		b.WriteString("(")
		b.WriteString(cap.DisplayName)
		b.WriteString(")")
	}
	if !cap.PublishedAt.IsZero() {
		b.WriteString(" · ")
		b.WriteString(cap.PublishedAt.UTC().Format("2006-01-02"))
	}
	return b.String()
}

// BuildTweetTitle renders a feed-list-friendly tweet title in the form
// "<DisplayName> · <first clause>". The first clause is the text up to the
// first sentence/clause break (.!?。！？,，) that falls within a useful
// rune-count range; absent a break we walk back to the last word boundary
// within 60 runes. Empty text falls back to "@handle 的推文" for
// image-only tweets. Final fallback is "Twitter 推文".
func BuildTweetTitle(cap *TweetCapture) string {
	// X Articles have their own title — use it verbatim (with a name prefix
	// when available) so the feed shows the article's real heading rather
	// than the first clause of its lead paragraph.
	if cap.ArticleTitle != "" {
		switch {
		case cap.DisplayName != "":
			return cap.DisplayName + " · " + cap.ArticleTitle
		case cap.Author != "":
			return "@" + cap.Author + " · " + cap.ArticleTitle
		default:
			return cap.ArticleTitle
		}
	}

	text := strings.TrimSpace(cap.TextMarkdown)
	if text == "" {
		if cap.DisplayName != "" {
			return cap.DisplayName + " 的推文"
		}
		if cap.Author != "" {
			return "@" + cap.Author + " 的推文"
		}
		return "Twitter 推文"
	}

	body := firstClauseOrTruncate(strings.ReplaceAll(text, "\n", " "))

	switch {
	case cap.DisplayName != "":
		return cap.DisplayName + " · " + body
	case cap.Author != "":
		return "@" + cap.Author + " · " + body
	default:
		return body
	}
}

// firstClauseOrTruncate returns text up to the first clause break
// (.!?。！？,，) whose rune index is in [8, 80] — short enough to read at a
// glance, long enough to not collapse on "+1!" or "lol". When no useful
// break exists, falls back to a 60-rune cap snapped to the last space in
// the upper half of the window (so we don't slice mid-word for English).
func firstClauseOrTruncate(text string) string {
	const minClause, maxClause = 8, 80
	runes := []rune(text)
	breakIdx := -1
	for i, r := range runes {
		if i < minClause {
			continue
		}
		if i > maxClause {
			break
		}
		switch r {
		case '.', '!', '?', '。', '！', '？', ',', '，':
			breakIdx = i
		}
		if breakIdx != -1 {
			break
		}
	}
	if breakIdx != -1 {
		return strings.TrimRight(string(runes[:breakIdx]), " ")
	}
	if len(runes) <= 60 {
		return text
	}
	chunk := string(runes[:60])
	if lastSpace := strings.LastIndex(chunk, " "); lastSpace > 30 {
		return strings.TrimRight(chunk[:lastSpace], " ") + "…"
	}
	return chunk + "…"
}
