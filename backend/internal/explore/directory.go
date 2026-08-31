package explore

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// DirectoryAdapter extracts public site links from Atom/RSS directory entries.
type DirectoryAdapter struct{}

func (DirectoryAdapter) Kind() string { return "directory" }

func (DirectoryAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}

	directoryHost := hostOf(provider.Endpoint)
	collector := newCandidateCollector(maxProviderCandidates, func(candidate Candidate) string { return candidate.FeedURL })
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	sawRoot, rootClosed := false, false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !sawRoot || depth != 0 {
				return nil, fmt.Errorf("malformed directory document")
			}
			return collector.candidates(), nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse directory: %w", err)
		}

		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootClosed {
					return nil, fmt.Errorf("malformed directory document: multiple root elements")
				}
				sawRoot = true
			}
			depth++
			switch value.Name.Local {
			case "entry":
				var entry atomDirectoryEntry
				if err := decoder.DecodeElement(&entry, &value); err != nil {
					return nil, fmt.Errorf("parse Atom entry: %w", err)
				}
				depth--
				if depth == 0 {
					rootClosed = true
				}
				addDirectoryCandidate(collector, provider, directoryHost, entry.candidate(directoryHost))
			case "item":
				var item rssDirectoryItem
				if err := decoder.DecodeElement(&item, &value); err != nil {
					return nil, fmt.Errorf("parse RSS item: %w", err)
				}
				depth--
				if depth == 0 {
					rootClosed = true
				}
				addDirectoryCandidate(collector, provider, directoryHost, item.candidate(directoryHost))
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
	}
}

func addDirectoryCandidate(collector *candidateCollector, provider Provider, directoryHost string, candidate Candidate) {
	if candidate.FeedURL == "" || hostOf(candidate.FeedURL) == directoryHost {
		return
	}
	candidate.Topic = provider.Topic
	candidate.Tags = append([]string{provider.Topic}, candidate.Tags...)
	candidate.OccurrenceCount = 1
	normalized, ok := normalizeCandidate(candidate)
	if ok {
		collector.add(normalized)
	}
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

func (entry atomDirectoryEntry) candidate(directoryHost string) Candidate {
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
	tags := make([]string, 0, len(entry.Categories))
	for _, category := range entry.Categories {
		tags = append(tags, category.Term)
	}
	return Candidate{ExternalKey: link, FeedURL: link, Title: strings.TrimSpace(entry.Title), Tags: tags}
}

type rssDirectoryItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	Categories []string `xml:"category"`
}

func (item rssDirectoryItem) candidate(directoryHost string) Candidate {
	return Candidate{ExternalKey: strings.TrimSpace(item.Link), FeedURL: strings.TrimSpace(item.Link), Title: strings.TrimSpace(item.Title), Tags: item.Categories}
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
