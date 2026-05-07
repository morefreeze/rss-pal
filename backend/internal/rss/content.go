package rss

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

var mdConverter = htmltomd.NewConverter(
	htmltomd.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(),
	),
)

// jinaFallbackMinChars is the threshold below which a successful direct
// extraction is still considered insufficient — likely a JS-rendered page or
// boilerplate-only response — and we try the Jina Reader fallback instead.
// Kept aligned with the worker's "short content" re-fetch threshold so the
// fallback is exercised before an article gets flagged for endless re-fetching.
const jinaFallbackMinChars = 300

type ContentFetcher struct {
	client     *http.Client
	jinaAPIKey string
}

func NewContentFetcher() *ContentFetcher {
	return &ContentFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		jinaAPIKey: os.Getenv("JINA_API_KEY"),
	}
}

// FetchContent fetches and extracts main content from a URL. If the direct
// fetch is blocked (non-2xx, e.g. Cloudflare 403) or yields too little text
// (likely a JS-rendered page), it falls back to Jina Reader (r.jina.ai).
func (f *ContentFetcher) FetchContent(ctx context.Context, url string) (string, error) {
	content, status, err := f.fetchDirect(ctx, url)
	if err != nil {
		// Network-level failure — try Jina before giving up.
		if jc, jerr := f.fetchViaJina(ctx, url); jerr == nil && jc != "" {
			return jc, nil
		}
		return "", err
	}

	if status == http.StatusOK && len(content) >= jinaFallbackMinChars {
		return content, nil
	}

	// Direct fetch was blocked or extracted too little — try Jina Reader.
	if jc, jerr := f.fetchViaJina(ctx, url); jerr == nil && jc != "" {
		return jc, nil
	} else if jerr != nil {
		log.Printf("Jina fallback failed for %s: %v", url, jerr)
	}

	return content, nil
}

// fetchDirect performs the original direct HTTP scrape. Returns the extracted
// content, the HTTP status code, and any transport error.
func (f *ContentFetcher) fetchDirect(ctx context.Context, url string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", 0, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, nil
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}

	// Remove unwanted elements
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Remove()
	StripAvatars(doc)

	// Try to find main content
	var content string

	// Try common content selectors (ordered from most specific to least)
	// Includes both English and Chinese news site class conventions
	selectors := []string{
		"article",
		"[role='main']",
		"main",
		".post-content",
		".article-content",
		".article-body",
		".entry-content",
		".story-body",
		".post-body",
		".field-item",
		// Chinese site common selectors
		".article-text",
		".article__body",
		".content-article",
		"[class*=article-detail]",
		"[class*=articleDetail]",
		"[class*=post-detail]",
		"[id*=article-body]",
		"[id*=articleBody]",
		"[id*=js_content]",   // WeChat articles
		".content",
		".post",
		"#content",
		"#main",
		"body",
	}

	for _, selector := range selectors {
		if doc.Find(selector).Length() > 0 {
			selection := doc.Find(selector).First()
			content = ExtractMarkdown(selection)
			if len(content) > 200 {
				break
			}
		}
	}

	if content == "" {
		// Fallback: get all paragraph text
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if len(text) > 30 {
				content += text + "\n\n"
			}
		})
	}

	// Clean up content
	content = cleanContent(content)

	// Limit content length
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}

	return content, http.StatusOK, nil
}

// fetchViaJina retrieves the article via the Jina Reader proxy
// (https://r.jina.ai/<url>), which executes the page in a real browser and
// returns clean markdown — useful for Cloudflare-protected or JS-rendered
// pages that block direct scraping. If an API key is configured but rejected
// (auth error / out of balance), retries once anonymously so the fallback
// still works on the free tier.
func (f *ContentFetcher) fetchViaJina(ctx context.Context, target string) (string, error) {
	if content, err := f.jinaRequest(ctx, target, f.jinaAPIKey); err == nil {
		return content, nil
	} else if f.jinaAPIKey == "" {
		return "", err
	} else {
		log.Printf("Jina request with API key failed (%v), retrying anonymously", err)
	}
	return f.jinaRequest(ctx, target, "")
}

func (f *ContentFetcher) jinaRequest(ctx context.Context, target, apiKey string) (string, error) {
	endpoint := "https://r.jina.ai/" + target
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jina reader returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(string(body))
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	return content, nil
}

// ExtractMarkdown converts the HTML inside the goquery selection into Markdown.
// Falls back to the selection's plain text if conversion fails (which should
// not happen under normal use but keeps the pipeline robust).
func ExtractMarkdown(selection *goquery.Selection) string {
	html, err := selection.Html()
	if err != nil || strings.TrimSpace(html) == "" {
		return strings.TrimSpace(selection.Text())
	}
	md, err := mdConverter.ConvertString(html)
	if err != nil {
		return strings.TrimSpace(selection.Text())
	}
	return strings.TrimSpace(md)
}

func cleanContent(content string) string {
	// Remove excessive whitespace
	re := regexp.MustCompile(`\n{3,}`)
	content = re.ReplaceAllString(content, "\n\n")

	// Remove multiple spaces
	re = regexp.MustCompile(` {2,}`)
	content = re.ReplaceAllString(content, " ")

	// Remove line-level junk (navigation/social text that snuck through)
	lines := strings.Split(content, "\n")
	var filtered []string
	junkContains := []string{
		"Subscribe to our newsletter", "Sign up for our", "Follow us on",
		"Share this article", "Read more:", "Click here to",
		"All rights reserved", "Terms of Service", "Privacy Policy",
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isJunk := false
		for _, junk := range junkContains {
			if strings.Contains(trimmed, junk) {
				isJunk = true
				break
			}
		}
		if !isJunk {
			filtered = append(filtered, line)
		}
	}
	content = strings.Join(filtered, "\n")

	return strings.TrimSpace(content)
}

// FetchContentFromReader extracts content from an io.Reader (for testing or reuse)
func (f *ContentFetcher) FetchContentFromReader(r io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", err
	}

	doc.Find("script, style, nav, header, footer, aside").Remove()
	StripAvatars(doc)

	selectors := []string{"article", "[role='main']", "main", ".content", ".post", "#content", "body"}
	var content string
	for _, sel := range selectors {
		if doc.Find(sel).Length() == 0 {
			continue
		}
		md := ExtractMarkdown(doc.Find(sel).First())
		if len(md) > 50 {
			content = md
			break
		}
	}

	if content == "" {
		// Last-resort paragraph fallback (kept for ultra-stripped pages)
		doc.Find("p").Each(func(_ int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if len(t) > 30 {
				content += t + "\n\n"
			}
		})
	}

	return cleanContent(content), nil
}

// StripHTML removes HTML tags from a string, returning plain text.
// Used to sanitize RSS description/content fields that may contain HTML markup.
func StripHTML(html string) string {
	if !strings.Contains(html, "<") {
		return html
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	text := doc.Text()
	return cleanContent(text)
}

// avatarAttrKeywords are case-insensitive substrings that, when found in an
// <img>'s class/id/alt/rel attributes, mark it as an author/profile avatar.
var avatarAttrKeywords = []string{
	"avatar", "gravatar", "profile", "author",
	"user-pic", "userpic", "headshot",
}

// avatarURLKeywords are case-insensitive substrings that, when found in an
// <img> src URL, mark it as an avatar.
var avatarURLKeywords = []string{
	"gravatar.com", "/avatar/", "/avatars/",
}

// avatarMaxDimension is the upper bound (inclusive) for an <img>'s declared
// width AND height to be treated as an avatar by dimension. Both dimensions
// must be present and parseable; one alone does not qualify.
const avatarMaxDimension = 64

// isAvatarImg reports whether an <img> selection looks like an author/profile
// avatar. Returns true on either signal: keyword/URL match (class/id/alt/rel/src)
// or both width AND height attributes present and ≤ avatarMaxDimension.
func isAvatarImg(s *goquery.Selection) bool {
	for _, attr := range []string{"class", "id", "alt", "rel"} {
		v := strings.ToLower(s.AttrOr(attr, ""))
		if v == "" {
			continue
		}
		for _, kw := range avatarAttrKeywords {
			if strings.Contains(v, kw) {
				return true
			}
		}
	}
	src := strings.ToLower(s.AttrOr("src", ""))
	for _, kw := range avatarURLKeywords {
		if strings.Contains(src, kw) {
			return true
		}
	}
	w, wErr := strconv.Atoi(strings.TrimSpace(s.AttrOr("width", "")))
	h, hErr := strconv.Atoi(strings.TrimSpace(s.AttrOr("height", "")))
	if wErr == nil && hErr == nil && w > 0 && h > 0 && w <= avatarMaxDimension && h <= avatarMaxDimension {
		return true
	}
	return false
}

// StripAvatars removes <img> elements matching avatar heuristics from doc,
// mutating it in place. Called before markdown conversion so avatars never
// enter stored content.
func StripAvatars(doc *goquery.Document) {
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if isAvatarImg(s) {
			s.Remove()
		}
	})
}

// stripJinaMathShadow removes the Unicode "shadow" that Jina Reader appends
// immediately after each LaTeX math span in scraped markdown. See
// docs/superpowers/specs/2026-05-07-math-formula-rendering-design.md for the
// detection rules. Pure function: idempotent and safe on inputs without math.
func stripJinaMathShadow(md string) string {
	r := []rune(md)
	var b strings.Builder
	b.Grow(len(md))
	i := 0
	for i < len(r) {
		if r[i] != '$' {
			b.WriteRune(r[i])
			i++
			continue
		}
		// Look for a closing $ on the same line.
		j := i + 1
		for j < len(r) && r[j] != '$' && r[j] != '\n' {
			j++
		}
		if j >= len(r) || r[j] == '\n' {
			b.WriteRune(r[i])
			i++
			continue
		}
		body := r[i+1 : j]
		if !mathBodyQualifies(body) {
			b.WriteRune(r[i])
			i++
			continue
		}
		// Emit $body$ verbatim.
		b.WriteString(string(r[i : j+1]))
		i = j + 1
		// Scan and possibly drop the shadow that follows.
		end, hasSignal := scanMathShadow(r, i)
		if hasSignal {
			i = end
		}
	}
	return b.String()
}

func mathBodyQualifies(body []rune) bool {
	if len(body) == 0 {
		return false
	}
	// Body starting with a digit is likely a price like $5, not LaTeX.
	if body[0] >= '0' && body[0] <= '9' {
		return false
	}
	for _, c := range body {
		switch c {
		case '\\', '{', '}', '_', '^':
			return true
		}
	}
	// A body without LaTeX specials may still be math if its shadow carries a
	// Unicode signal (e.g. "$x - 1$x−1"). We accept such bodies here and rely
	// on scanMathShadow's hasSignal to decide whether to actually drop them.
	return true
}

func scanMathShadow(r []rune, start int) (end int, hasSignal bool) {
	end = start
	for end < len(r) {
		c := r[end]
		if c == '\n' {
			break
		}
		// Sentence-level punctuation (comma, period, etc.) terminates the shadow
		// only when followed by whitespace, another $, or end of input — meaning
		// it is sentence punctuation rather than part of a compact notation like
		// "3+7​=10​,3-1=2".
		if c == ',' || c == '.' || c == ';' || c == ':' || c == '!' || c == '?' {
			next := end + 1
			if next >= len(r) || r[next] == ' ' || r[next] == '\t' || r[next] == '\n' || r[next] == '$' {
				break
			}
		}
		if isAsciiLetterRune(c) {
			k := end
			for k < len(r) && isAsciiLetterRune(r[k]) {
				k++
			}
			if k-end >= 3 {
				break
			}
			end = k
			continue
		}
		if isMathSignalRune(c) {
			hasSignal = true
		}
		end++
	}
	for end > start && (r[end-1] == ' ' || r[end-1] == '\t') {
		end--
	}
	return end, hasSignal
}

func isAsciiLetterRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isMathSignalRune(r rune) bool {
	switch {
	case r == 0x200B,
		r == 0x2212:
		return true
	case r >= 0x00B0 && r <= 0x00FF,
		r >= 0x2200 && r <= 0x23FF,
		r >= 0x2A00 && r <= 0x2AFF,
		r >= 0x2070 && r <= 0x209F,
		r >= 0x0391 && r <= 0x03C9:
		return true
	}
	return false
}
