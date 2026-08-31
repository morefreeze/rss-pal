package explore

import (
	"encoding/xml"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// RedditLinkStreamAdapter extracts linked public sites from a Reddit RSSHub
// stream; Reddit/RSSHub entries themselves are never candidates.
type RedditLinkStreamAdapter struct{}

func (RedditLinkStreamAdapter) Kind() string { return "reddit_stream" }

func (RedditLinkStreamAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	var document struct {
		Items []struct {
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Content     string `xml:"content"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		return nil, err
	}
	byDomain := map[string]Candidate{}
	for _, item := range document.Items {
		for _, raw := range extractHTTPURLs(item.Link + "\n" + item.Description + "\n" + item.Content) {
			normalized, ok := normalizePublicURL(raw)
			if !ok || ignoredRedditURL(normalized) {
				continue
			}
			domain := hostOf(normalized)
			candidate, found := byDomain[domain]
			if !found || normalized < candidate.FeedURL {
				candidate = Candidate{ExternalKey: domain, FeedURL: normalized, Title: domain, Topic: provider.Topic, Tags: []string{provider.Topic}}
			}
			candidate.OccurrenceCount++
			byDomain[domain] = candidate
		}
	}
	out := make([]Candidate, 0, len(byDomain))
	for _, candidate := range byDomain {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FeedURL < out[j].FeedURL })
	return out, nil
}

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>'"()]+`)

func extractHTTPURLs(text string) []string { return httpURLPattern.FindAllString(text, -1) }

func ignoredRedditURL(raw string) bool {
	host := hostOf(raw)
	if host == "reddit.com" || strings.HasSuffix(host, ".reddit.com") || host == "redd.it" || strings.HasSuffix(host, ".redd.it") || host == "rsshub.app" || strings.HasSuffix(host, ".rsshub.app") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	ext := strings.ToLower(u.Path)
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".mp4", ".webm", ".mp3", ".pdf"} {
		if strings.HasSuffix(ext, suffix) {
			return true
		}
	}
	return false
}
