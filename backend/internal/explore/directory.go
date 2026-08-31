package explore

import (
	"encoding/xml"
	"net/url"
	"strings"
)

// DirectoryAdapter extracts public site links from Atom/RSS directory entries.
type DirectoryAdapter struct{}

func (DirectoryAdapter) Kind() string { return "directory" }

func (DirectoryAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	var feed directoryDocument
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	directoryHost := hostOf(provider.Endpoint)
	candidates := make([]Candidate, 0, len(feed.Entries)+len(feed.RSSItems))
	for _, entry := range feed.Entries {
		link := ""
		for _, current := range entry.Links {
			if current.Href == "" || hostOf(current.Href) == directoryHost {
				continue
			}
			if current.Rel == "" || strings.EqualFold(current.Rel, "alternate") {
				link = current.Href
				break
			}
		}
		if link == "" {
			continue
		}
		tags := []string{provider.Topic}
		for _, category := range entry.Categories {
			tags = append(tags, category.Term)
		}
		candidates = append(candidates, Candidate{ExternalKey: link, FeedURL: link, Title: strings.TrimSpace(entry.Title), Topic: provider.Topic, Tags: tags})
	}
	for _, item := range feed.RSSItems {
		if item.Link == "" || hostOf(item.Link) == directoryHost {
			continue
		}
		tags := []string{provider.Topic}
		for _, category := range item.Categories {
			tags = append(tags, category)
		}
		candidates = append(candidates, Candidate{ExternalKey: item.Link, FeedURL: item.Link, Title: strings.TrimSpace(item.Title), Topic: provider.Topic, Tags: tags})
	}
	return NormalizeCandidates(candidates), nil
}

type directoryDocument struct {
	Entries  []atomDirectoryEntry `xml:"entry"`
	RSSItems []rssDirectoryItem   `xml:"channel>item"`
}
type atomDirectoryEntry struct {
	Title string `xml:"title"`
	Links []struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}
type rssDirectoryItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	Categories []string `xml:"category"`
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
