package explore

import (
	"encoding/xml"
	"strings"
)

// OPMLRegistryAdapter parses OPML registries without fetching their sources.
type OPMLRegistryAdapter struct{}

func (OPMLRegistryAdapter) Kind() string { return "opml" }

func (OPMLRegistryAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	var document struct {
		Body struct {
			Outlines []opmlOutline `xml:"outline"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0)
	for _, outline := range document.Body.Outlines {
		collectOPMLOutline(&candidates, provider, outline, nil)
	}
	return NormalizeCandidates(candidates), nil
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	HTMLURL  string        `xml:"htmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

func collectOPMLOutline(out *[]Candidate, provider Provider, outline opmlOutline, path []string) {
	name := strings.TrimSpace(outline.Title)
	if name == "" {
		name = strings.TrimSpace(outline.Text)
	}
	nextPath := path
	if outline.XMLURL == "" && name != "" {
		nextPath = append(append([]string{}, path...), name)
	}
	if outline.XMLURL != "" {
		tags := append([]string{}, path...)
		if provider.Topic != "" {
			tags = append(tags, provider.Topic)
		}
		*out = append(*out, Candidate{ExternalKey: outline.XMLURL, FeedURL: outline.XMLURL, SiteURL: outline.HTMLURL, Title: name, Topic: provider.Topic, Tags: tags})
	}
	for _, child := range outline.Outlines {
		collectOPMLOutline(out, provider, child, nextPath)
	}
}
