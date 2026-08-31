package explore

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// RedditLinkStreamAdapter extracts linked public sites from a Reddit RSSHub
// stream; Reddit/RSSHub entries themselves are never candidates.
type RedditLinkStreamAdapter struct{}

func (RedditLinkStreamAdapter) Kind() string { return "reddit_stream" }

func (RedditLinkStreamAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}

	collector := newCandidateCollector(maxProviderCandidates, func(candidate Candidate) string { return candidate.ExternalKey })
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	sawRoot, rootClosed := false, false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !sawRoot || depth != 0 {
				return nil, fmt.Errorf("malformed Reddit RSS document")
			}
			return collector.candidates(), nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse Reddit RSS: %w", err)
		}

		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootClosed {
					return nil, fmt.Errorf("malformed Reddit RSS document: multiple root elements")
				}
				sawRoot = true
			}
			depth++
			if value.Name.Local != "item" {
				continue
			}
			var item redditRSSItem
			if err := decoder.DecodeElement(&item, &value); err != nil {
				return nil, fmt.Errorf("parse Reddit RSS item: %w", err)
			}
			depth--
			addRedditItem(collector, provider, item)
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
	}
}

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>'"()]+`)

type redditRSSItem struct {
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Content     string `xml:"content"`
}

func addRedditItem(collector *candidateCollector, provider Provider, item redditRSSItem) {
	for _, text := range []string{item.Link, item.Description, item.Content} {
		for cursor := 0; cursor < len(text); {
			match := httpURLPattern.FindStringIndex(text[cursor:])
			if match == nil {
				break
			}
			start, end := cursor+match[0], cursor+match[1]
			cursor = end
			normalized, ok := normalizePublicURL(text[start:end])
			if !ok || ignoredRedditURL(normalized) {
				continue
			}
			domain := hostOf(normalized)
			collector.add(Candidate{
				ExternalKey:     domain,
				FeedURL:         normalized,
				Title:           domain,
				Topic:           provider.Topic,
				Tags:            []string{provider.Topic},
				OccurrenceCount: 1,
			})
		}
	}
}

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
