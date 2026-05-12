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
	TextMarkdown string    // tweet text rendered as markdown
	ImageURLs    []string  // pbs.twimg.com URLs, upgraded to ?name=large
	QuoteURL     string    // x.com permalink of quoted tweet, normalized
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
		TextMarkdown: extractTweetText(focal),
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

// extractTweetText walks the [data-testid="tweetText"] subtree and renders
// it as markdown. Twitter's tweetText is a sequence of spans and anchors;
// emoji are rendered as <img alt="..."> whose `alt` is the emoji char.
func extractTweetText(focal *goquery.Selection) string {
	textNode := focal.Find(`[data-testid="tweetText"]`).First()
	if textNode.Length() == 0 {
		return ""
	}
	var b strings.Builder
	walkTextMarkdown(textNode, &b)
	// Collapse runs of >2 newlines and trim trailing whitespace.
	out := strings.TrimSpace(b.String())
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return out
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
		dt, _ := tm.Attr("datetime")
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			ts = t.UTC()
			return false
		}
		return true
	})
	return ts
}
