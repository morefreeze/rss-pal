package rss

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// twitterHosts is the set of canonical and legacy hosts that serve Twitter /
// X content. Mobile and www subdomains are accepted because users land on
// them depending on the device and the link source.
var twitterHosts = map[string]struct{}{
	"x.com":              {},
	"www.x.com":          {},
	"twitter.com":        {},
	"www.twitter.com":    {},
	"mobile.twitter.com": {},
}

// twitterStatusPathRe matches a single-tweet permalink path. The handle is
// limited to Twitter's documented 15-char max plus `_` to avoid swallowing
// internal `/i/...` system routes.
var twitterStatusPathRe = regexp.MustCompile(`^/[A-Za-z0-9_]{1,15}/status/([0-9]+)/?$`)

// twitterPermalinkPathRe matches either a tweet (`/status/`) or X Article
// (`/article/`) permalink — both forms appear as the clickable destination
// of a quote card.
var twitterPermalinkPathRe = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/(status|article)/([0-9]+)/?$`)

// IsTwitterStatusURL reports whether raw is a Twitter / X single-tweet
// permalink. On match it returns the numeric tweet id from the path so the
// caller can pin DOM extraction to the focal tweet (a tweet page renders
// many tweets in the conversation; only one matches the URL).
func IsTwitterStatusURL(raw string) (statusID string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if _, ok := twitterHosts[host]; !ok {
		return "", false
	}
	m := twitterStatusPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// TweetCapture is the structured result of parsing a logged-in Twitter / X
// page's HTML for a single focal tweet. Empty fields mean "not present on the
// page"; callers decide whether to render or skip each section.
type TweetCapture struct {
	Author       string    // handle without leading @, e.g. "karpathy"
	DisplayName  string    // display name, e.g. "Andrej Karpathy"
	PublishedAt  time.Time // RFC3339 from <time datetime="...">, zero if absent
	ArticleTitle string    // X Article title (data-testid="twitter-article-title"); empty for regular tweets
	TextMarkdown string    // tweet text rendered as markdown — for X Articles this is the longform body
	ImageURLs    []string  // pbs.twimg.com URLs, upgraded to ?name=large
	Quote        *Quote    // nested quoted tweet/article, nil if focal has no quote
}

// Quote is the structured contents of a quote card embedded in the focal
// tweet. URL is always set when Quote != nil; the other fields are
// best-effort and degrade rendering when empty (e.g. an X Article quote
// where we can't extract the title spans).
type Quote struct {
	URL         string    // permalink of quoted tweet or X Article
	Author      string    // handle of quoted user (without @)
	DisplayName string    // display name of quoted user
	PublishedAt time.Time // <time datetime="..."> inside the quote card
	Excerpt     string    // visible body text of the card, capped at ~280 runes
}

// ErrTweetNotFound means the focal tweet identified by statusID was not
// present in the HTML, or was so degenerate that no field could be extracted.
// Callers fall back to the generic extractor so the capture still produces an
// article rather than a 422.
var ErrTweetNotFound = errors.New("twitter: focal tweet not found in html")

// ExtractTweet parses html (typically the full document the bookmarklet
// shipped) and returns the focal tweet identified by statusID. The function
// is pure: no I/O, no global state.
func ExtractTweet(html string, statusID string) (*TweetCapture, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("twitter: parse html: %w", err)
	}

	focal := findFocalTweet(doc, statusID)
	if focal == nil {
		return nil, ErrTweetNotFound
	}

	out := &TweetCapture{
		Author:       extractAuthorHandle(focal),
		DisplayName:  extractDisplayName(focal),
		PublishedAt:  extractPublishedAt(focal),
		ArticleTitle: extractArticleTitle(focal),
		TextMarkdown: extractFocalBody(focal),
		ImageURLs:    extractTweetImages(focal),
		Quote:        extractQuote(focal, statusID),
	}
	return out, nil
}

// findFocalTweet picks the <article> element that represents the tweet whose
// permalink matches statusID. A conversation page may include the focal
// tweet, replies, and ancestors; the focal tweet is the one whose permalink
// link matches the URL the user is looking at, with tabindex="-1" as a
// tiebreaker (Twitter marks the focal tweet that way).
func findFocalTweet(doc *goquery.Document, statusID string) *goquery.Selection {
	wantSuffix := "/status/" + statusID
	var match *goquery.Selection
	doc.Find(`article[role="article"][data-testid="tweet"]`).EachWithBreak(func(_ int, art *goquery.Selection) bool {
		// Does this article contain a permalink to our statusID?
		found := false
		art.Find(`a[href]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			if strings.HasSuffix(href, wantSuffix) || strings.HasSuffix(href, wantSuffix+"/") {
				found = true
				return false
			}
			return true
		})
		if !found {
			return true
		}
		// Prefer the focal one (tabindex="-1") over context tweets that
		// happen to share the same id (rare but possible with quoted tweets).
		if tabindex, _ := art.Attr("tabindex"); tabindex == "-1" {
			match = art
			return false
		}
		if match == nil {
			match = art
		}
		return true
	})
	return match
}

// extractFocalBody returns the focal post's main body as markdown. For X
// Articles (long-form posts) we walk the DraftJS-style
// [data-testid="longformRichTextComponent"] subtree, preserving paragraph
// breaks, list items, and bold runs. For regular tweets we walk
// [data-testid="tweetText"] as before.
func extractFocalBody(focal *goquery.Selection) string {
	if longform := focal.Find(`[data-testid="longformRichTextComponent"]`).First(); longform.Length() > 0 {
		var b strings.Builder
		walkLongform(longform, &b, false, false)
		return collapseBlankLines(normalizeLongformLines(b.String()))
	}
	textNode := focal.Find(`[data-testid="tweetText"]`).First()
	if textNode.Length() == 0 {
		return ""
	}
	var b strings.Builder
	walkTextMarkdown(textNode, &b)
	return collapseBlankLines(strings.TrimSpace(b.String()))
}

// extractArticleTitle returns the heading of an X Article post, taken from
// the [data-testid="twitter-article-title"] element inside the focal
// article. Regular tweets have no such element and return "".
func extractArticleTitle(focal *goquery.Selection) string {
	title := focal.Find(`[data-testid="twitter-article-title"]`).First()
	if title.Length() == 0 {
		return ""
	}
	return strings.TrimSpace(title.Text())
}

// walkLongform renders an X Article body (DraftJS-style DOM) as markdown.
// Paragraph blocks (`public-DraftStyleDefault-block` divs) close with a
// blank line, list items (`<li>`) emit `- ` prefix, and any element whose
// inline style sets `font-weight: bold` wraps its text in `**…**`. The
// caller-supplied builder accumulates output; inBold threads through
// recursion so a bold ancestor styles all enclosed text runs.
func walkLongform(sel *goquery.Selection, b *strings.Builder, inBold, inListItem bool) {
	sel.Contents().Each(func(_ int, n *goquery.Selection) {
		node := n.Get(0)
		if node == nil {
			return
		}
		if node.Type == html.TextNode {
			if inBold && strings.TrimSpace(node.Data) != "" {
				b.WriteString("**")
				b.WriteString(node.Data)
				b.WriteString("**")
			} else {
				b.WriteString(node.Data)
			}
			return
		}
		if node.Type != html.ElementNode {
			return
		}
		style, _ := n.Attr("style")
		nowBold := inBold || strings.Contains(style, "font-weight: bold") || strings.Contains(style, "font-weight:bold")

		switch node.Data {
		case "br":
			b.WriteString("\n")
		case "li":
			// Buffer the item body into a sub-builder so we can collapse
			// indent whitespace and inline the contained block. Otherwise
			// the indented text node between <li> and its inner <div>
			// breaks the line right after the bullet marker.
			var item strings.Builder
			walkLongform(n, &item, nowBold, true)
			text := strings.ReplaceAll(strings.TrimSpace(item.String()), "\n", " ")
			text = collapseInlineSpaces(text)
			if text != "" {
				b.WriteString("\n- ")
				b.WriteString(text)
			}
		case "ul", "ol":
			b.WriteString("\n")
			walkLongform(n, b, nowBold, inListItem)
			b.WriteString("\n")
		case "a":
			href, _ := n.Attr("href")
			inner := strings.TrimSpace(n.Text())
			if href != "" && inner != "" {
				if nowBold {
					fmt.Fprintf(b, "**[%s](%s)**", inner, href)
				} else {
					fmt.Fprintf(b, "[%s](%s)", inner, href)
				}
			} else {
				walkLongform(n, b, nowBold, inListItem)
			}
		case "div":
			class, _ := n.Attr("class")
			isBlock := strings.Contains(class, "DraftStyleDefault-block")
			walkLongform(n, b, nowBold, inListItem)
			// Paragraph breaks belong to standalone blocks. A block inside
			// a list item is just the item's body; the surrounding <li>
			// already provides the line break.
			if isBlock && !inListItem {
				b.WriteString("\n\n")
			}
		default:
			walkLongform(n, b, nowBold, inListItem)
		}
	})
}

func walkTextMarkdown(sel *goquery.Selection, b *strings.Builder) {
	sel.Contents().Each(func(_ int, n *goquery.Selection) {
		node := n.Get(0)
		if node == nil {
			return
		}
		switch node.Type {
		case html.TextNode:
			b.WriteString(node.Data)
		case html.ElementNode:
			switch node.Data {
			case "br":
				b.WriteString("\n")
			case "a":
				href, _ := n.Attr("href")
				inner := strings.TrimSpace(n.Text())
				if href != "" && inner != "" {
					fmt.Fprintf(b, "[%s](%s)", inner, href)
				} else {
					b.WriteString(inner)
				}
			case "img":
				if alt, ok := n.Attr("alt"); ok {
					b.WriteString(alt)
				}
			default:
				walkTextMarkdown(n, b)
			}
		}
	})
}

// extractAuthorHandle reads the first profile anchor inside the focal
// article. The href is always /<handle> (no nested path) for the byline
// link. Falls back to empty string — the caller may still know the handle
// from the URL.
var profileHrefRe = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})$`)

func extractAuthorHandle(focal *goquery.Selection) string {
	var handle string
	focal.Find(`[data-testid="User-Name"] a[href][role="link"]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if hasAncestor(a, focal, `div[role="link"]`) {
			return true // inside the quote card — belongs to the quoted user, not focal
		}
		href, _ := a.Attr("href")
		if m := profileHrefRe.FindStringSubmatch(href); m != nil {
			handle = m[1]
			return false
		}
		return true
	})
	return handle
}

// extractDisplayName takes the first non-empty <span> text inside the
// User-Name container. Twitter renders the display name first, then the
// `@handle` in a second span; trimming and stopping at the first useful
// value picks the display name.
func extractDisplayName(focal *goquery.Selection) string {
	var name string
	focal.Find(`[data-testid="User-Name"] span`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if hasAncestor(s, focal, `div[role="link"]`) {
			return true // quoted user's name, not focal's
		}
		txt := strings.TrimSpace(s.Text())
		if txt == "" || strings.HasPrefix(txt, "@") {
			return true
		}
		name = txt
		return false
	})
	return name
}

// extractPublishedAt parses the first <time datetime="..."> inside the
// focal article. Twitter renders it as RFC3339 / ISO 8601 UTC. Failure
// returns the zero time; callers leave Article.PublishedAt nil in that case.
func extractPublishedAt(focal *goquery.Selection) time.Time {
	var ts time.Time
	focal.Find(`time[datetime]`).EachWithBreak(func(_ int, tm *goquery.Selection) bool {
		if hasAncestor(tm, focal, `div[role="link"]`) {
			return true // quote card's timestamp, not focal's
		}
		dt, _ := tm.Attr("datetime")
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			ts = t.UTC()
			return false
		}
		return true
	})
	return ts
}

// extractTweetImages collects photo URLs from the focal tweet, excluding:
//   - profile avatars (path contains /profile_images/)
//   - images inside any role="link" container (these are quote-tweet
//     thumbnails; the quote tweet itself is captured separately as a link).
//
// Each URL has its `name=...` query parameter rewritten to `name=large` to
// pull the highest-quality variant Twitter serves anonymously.
func extractTweetImages(focal *goquery.Selection) []string {
	var urls []string
	focal.Find(`[data-testid="tweetPhoto"] img[src]`).Each(func(_ int, img *goquery.Selection) {
		// Skip if any ancestor up to focal has role="link" (quote card).
		if hasAncestor(img, focal, `div[role="link"]`) {
			return
		}
		src, _ := img.Attr("src")
		if !strings.Contains(src, "pbs.twimg.com") || strings.Contains(src, "/profile_images/") {
			return
		}
		urls = append(urls, upgradeTwitterImageURL(src))
	})
	return urls
}

// hasAncestor reports whether sel has an ancestor matching selector within
// the subtree rooted at stop (exclusive).
func hasAncestor(sel, stop *goquery.Selection, selector string) bool {
	stopNode := stop.Get(0)
	cur := sel.Parent()
	for cur.Length() > 0 {
		if cur.Get(0) == stopNode {
			return false
		}
		if cur.Is(selector) {
			return true
		}
		cur = cur.Parent()
	}
	return false
}

// extractQuote returns the structured contents of a quote card embedded in
// the focal tweet, or nil when no quote card is present. Twitter renders
// the card inside a [role="link"] div whose first anchor points at the
// quoted tweet's `/status/` or X Article's `/article/` permalink. The
// focal tweet's own permalink anchor lives outside any role="link"
// container, so it can't be mistaken for a quote.
func extractQuote(focal *goquery.Selection, focalStatusID string) *Quote {
	var out *Quote
	focal.Find(`div[role="link"]`).EachWithBreak(func(_ int, card *goquery.Selection) bool {
		url, author := pickQuotePermalink(card, focalStatusID)
		if url == "" {
			return true
		}
		out = &Quote{
			URL:         url,
			Author:      author,
			DisplayName: extractCardDisplayName(card),
			PublishedAt: extractCardPublishedAt(card),
			Excerpt:     extractCardExcerpt(card),
		}
		return false
	})
	return out
}

// pickQuotePermalink walks the card's anchors and returns the first
// permalink-shaped href that isn't the focal tweet's own. Returns the
// absolute URL and the handle parsed from the path.
func pickQuotePermalink(card *goquery.Selection, focalStatusID string) (url, author string) {
	card.Find(`a[href]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		m := twitterPermalinkPathRe.FindStringSubmatch(href)
		if m == nil {
			return true
		}
		// Skip if this is the focal's own /status/<id> link (a tweet can
		// link to itself from inside a card-shaped container in some
		// embedded contexts).
		if m[2] == "status" && m[3] == focalStatusID {
			return true
		}
		url = "https://x.com" + strings.TrimSuffix(href, "/")
		author = m[1]
		return false
	})
	return url, author
}

// extractCardDisplayName grabs the quoted user's display name from the
// card's inner User-Name container, mirroring the focal extractor's logic
// but scoped to the card subtree.
func extractCardDisplayName(card *goquery.Selection) string {
	var name string
	card.Find(`[data-testid="User-Name"] span`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := strings.TrimSpace(s.Text())
		if txt == "" || strings.HasPrefix(txt, "@") {
			return true
		}
		name = txt
		return false
	})
	return name
}

// extractCardPublishedAt parses the first <time datetime="..."> inside the
// card subtree.
func extractCardPublishedAt(card *goquery.Selection) time.Time {
	var ts time.Time
	card.Find(`time[datetime]`).EachWithBreak(func(_ int, tm *goquery.Selection) bool {
		dt, _ := tm.Attr("datetime")
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			ts = t.UTC()
			return false
		}
		return true
	})
	return ts
}

// extractCardExcerpt returns up to ~280 runes of visible body text from
// the quote card. Prefers the card's own [data-testid="tweetText"]; for X
// Article quotes (no tweetText element) it falls back to scraping the
// card's text content with the User-Name container and timestamps removed.
func extractCardExcerpt(card *goquery.Selection) string {
	if txt := card.Find(`[data-testid="tweetText"]`).First(); txt.Length() > 0 {
		var b strings.Builder
		walkTextMarkdown(txt, &b)
		return truncateRunes(collapseBlankLines(strings.TrimSpace(b.String())), 280)
	}
	return truncateRunes(collapseBlankLines(strings.TrimSpace(scrapeCardText(card))), 280)
}

// scrapeCardText walks the card subtree and concatenates text nodes,
// skipping descendants of the User-Name and time elements (those are meta
// already captured in DisplayName/PublishedAt).
func scrapeCardText(card *goquery.Selection) string {
	var b strings.Builder
	var walk func(sel *goquery.Selection)
	walk = func(sel *goquery.Selection) {
		sel.Contents().Each(func(_ int, n *goquery.Selection) {
			node := n.Get(0)
			if node == nil {
				return
			}
			if node.Type == html.TextNode {
				b.WriteString(node.Data)
				return
			}
			if node.Type != html.ElementNode {
				return
			}
			if n.Is(`[data-testid="User-Name"]`) || n.Is(`time`) {
				return
			}
			if node.Data == "br" {
				b.WriteString("\n")
				return
			}
			walk(n)
			// Block-ish elements get a soft break so adjacent paragraphs
			// don't run together as one wall of text.
			switch node.Data {
			case "div", "p", "h1", "h2", "h3", "h4", "section", "article":
				b.WriteString("\n")
			}
		})
	}
	walk(card)
	return b.String()
}

// truncateRunes returns s capped at max runes, appending an ellipsis when
// truncation actually happens. Pure, panic-free on multi-byte input.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// normalizeLongformLines trims each line and drops lines that are pure
// whitespace. Source HTML is heavily indented, so the walker inevitably
// captures formatting whitespace between block-level elements; we don't
// want that surfacing as runs of spaces or stranded `- ` bullet markers
// inside the markdown body.
func normalizeLongformLines(s string) string {
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if i > 0 {
			b.WriteString("\n")
		}
		// Collapse internal runs of whitespace to a single space so multi-
		// span paragraphs don't render with double-spaced text.
		b.WriteString(collapseInlineSpaces(trimmed))
	}
	return strings.TrimSpace(b.String())
}

// collapseInlineSpaces compacts runs of whitespace within a single line to
// one space. Leaves the empty string alone.
func collapseInlineSpaces(line string) string {
	if line == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range line {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// collapseBlankLines reduces 3+ consecutive newlines to exactly two, which
// is how markdown renders a paragraph break. Keeps the excerpt readable
// when the source DOM contains stacked empty divs.
func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// upgradeTwitterImageURL rewrites the `name=` query param to `large`, which
// is the highest-resolution variant Twitter serves to anonymous callers. If
// there's no `name=` param, the URL is returned as-is.
func upgradeTwitterImageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if _, has := q["name"]; !has {
		return raw
	}
	q.Set("name", "large")
	u.RawQuery = q.Encode()
	return u.String()
}
