package rss

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Candidate is one outbound link extracted from a link_set parent's body.
type Candidate struct {
	Title      string
	URL        string // absolute, normalised
	EditorNote string // ≤ 280 runes
}

var (
	linkSetUTMRe        = regexp.MustCompile(`(?i)^(utm_[a-z]+|ref|mc_cid|mc_eid|fbclid|gclid)$`)
	linkSetWhitespaceRe = regexp.MustCompile(`\s+`)
	linkSetStopwords    = map[string]struct{}{
		"here": {}, "click": {}, "read more": {}, "more": {}, "link": {},
		"→": {}, "↗": {}, "↗️": {},
		"comments": {}, "comments→": {}, "comments →": {},
		"discuss": {}, "discussion": {},
		"via": {}, "source": {},
	}
)

func ExtractCandidates(html, parentURL string) []Candidate {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []Candidate
	walkCandidates(doc, parentURL, func(c Candidate, _ *goquery.Selection) {
		out = append(out, c)
	})
	return out
}

// walkCandidates applies the standard link-set filter to every <a> in doc and
// invokes visit() for each kept candidate, passing the goquery selection so
// callers can also inspect DOM position. Shared by ExtractCandidates and
// DetectLinkSetSuggestion so the filter stays in one place.
func walkCandidates(doc *goquery.Document, parentURL string, visit func(Candidate, *goquery.Selection)) {
	parent, _ := url.Parse(parentURL)
	parentHost := ""
	parentDepth := 0
	if parent != nil {
		parentHost = strings.ToLower(parent.Hostname())
		parentDepth = linkSetPathDepth(parent.Path)
	}
	seen := make(map[string]struct{})

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") ||
			strings.HasPrefix(strings.ToLower(href), "mailto:") ||
			strings.HasPrefix(strings.ToLower(href), "tel:") ||
			strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return
		}
		abs := resolveURL(parentURL, href)
		if abs == "" {
			return
		}
		u, err := url.Parse(abs)
		if err != nil || u.Host == "" {
			return
		}
		host := strings.ToLower(u.Hostname())

		if parentHost != "" && (host == parentHost || strings.HasSuffix(host, "."+parentHost)) {
			if linkSetPathDepth(u.Path) < parentDepth {
				return
			}
		}
		if isLinkSetNoise(u) {
			return
		}
		title := strings.TrimSpace(linkSetWhitespaceRe.ReplaceAllString(s.Text(), " "))
		if title == "" {
			return
		}
		if _, isStop := linkSetStopwords[strings.ToLower(title)]; isStop {
			return
		}
		if len([]rune(title)) < 2 && s.Find("img").Length() > 0 && s.Children().Not("img").Length() == 0 {
			return
		}

		normalised := normaliseLinkSetURL(u)
		if _, dup := seen[normalised]; dup {
			return
		}
		seen[normalised] = struct{}{}

		note := extractEditorNote(s, title)
		visit(Candidate{Title: title, URL: normalised, EditorNote: note}, s)
	})
}

// linkSetPathDepth returns the number of non-empty path segments in a URL path.
// "/" → 0, "/x" → 1, "/x/y" → 2, etc.
func linkSetPathDepth(path string) int {
	p := strings.Trim(path, "/")
	if p == "" {
		return 0
	}
	return len(strings.Split(p, "/"))
}

func isLinkSetNoise(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	if host == "t.co" {
		return true
	}
	if host == "twitter.com" || host == "x.com" {
		if strings.HasPrefix(path, "/intent/") || strings.HasPrefix(path, "/share") {
			return true
		}
	}
	if (host == "facebook.com" || host == "www.facebook.com") && strings.HasPrefix(path, "/sharer") {
		return true
	}
	if host == "linkedin.com" || host == "www.linkedin.com" {
		if strings.HasPrefix(path, "/sharearticle") || strings.HasPrefix(path, "/sharing/share-offsite") {
			return true
		}
	}
	if (host == "reddit.com" || host == "www.reddit.com") && strings.HasPrefix(path, "/submit") {
		return true
	}
	if host == "static.addtoany.com" || strings.HasSuffix(host, ".addtoany.com") {
		return true
	}
	if strings.Contains(path, "/unsubscribe") || strings.Contains(path, "/manage-subscription") ||
		strings.Contains(path, "/preferences") {
		return true
	}
	if host == "buttondown.com" && strings.HasPrefix(path, "/emails/") {
		return true
	}
	return false
}

func normaliseLinkSetURL(u *url.URL) string {
	out := *u
	out.Host = strings.ToLower(out.Host)
	out.Fragment = ""
	q := out.Query()
	for k := range q {
		if linkSetUTMRe.MatchString(k) {
			q.Del(k)
		}
	}
	out.RawQuery = q.Encode()
	if len(out.Path) > 1 && strings.HasSuffix(out.Path, "/") {
		out.Path = strings.TrimRight(out.Path, "/")
	}
	return out.String()
}

func extractEditorNote(s *goquery.Selection, linkText string) string {
	container := s.Closest("li, p, dd").First()
	if container.Length() == 0 {
		sib := s.Parent()
		if sib.Length() > 0 {
			text := sib.Text()
			text = strings.Replace(text, linkText, "", 1)
			text = linkSetWhitespaceRe.ReplaceAllString(text, " ")
			text = strings.Trim(text, " —–-:　")
			return capEditorNote(text)
		}
		return ""
	}
	text := container.Text()
	text = strings.Replace(text, linkText, "", 1)
	text = linkSetWhitespaceRe.ReplaceAllString(text, " ")
	text = strings.Trim(text, " —–-:　")
	return capEditorNote(text)
}

func capEditorNote(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 280 {
		return string(runes[:280])
	}
	return s
}
