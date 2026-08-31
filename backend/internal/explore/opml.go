package explore

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// OPMLRegistryAdapter parses OPML registries without fetching their sources.
type OPMLRegistryAdapter struct{}

func (OPMLRegistryAdapter) Kind() string { return "opml" }

func (OPMLRegistryAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}

	collector := newCandidateCollector(maxProviderCandidates, func(candidate Candidate) string { return candidate.FeedURL })
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var categories []string
	var outlines []opmlOutlineContext
	bodyDepth, depth := 0, 0
	sawRoot, rootClosed := false, false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !sawRoot || depth != 0 {
				return nil, fmt.Errorf("malformed OPML document")
			}
			return collector.candidates(), nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse OPML: %w", err)
		}

		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootClosed {
					return nil, fmt.Errorf("malformed OPML document: multiple root elements")
				}
				sawRoot = true
			}
			depth++
			if value.Name.Local == "body" {
				bodyDepth++
			}
			if value.Name.Local != "outline" || bodyDepth == 0 {
				continue
			}
			outline := readOPMLOutline(value)
			context := opmlOutlineContext{}
			if outline.xmlURL == "" && outline.name != "" {
				categories = append(categories, outline.name)
				context.isCategory = true
			}
			outlines = append(outlines, context)
			if outline.xmlURL == "" {
				continue
			}
			tags := append([]string{}, categories...)
			tags = append(tags, outline.categories...)
			if provider.Topic != "" {
				tags = append(tags, provider.Topic)
			}
			candidate, ok := normalizeCandidate(Candidate{
				ExternalKey:     outline.xmlURL,
				FeedURL:         outline.xmlURL,
				SiteURL:         outline.htmlURL,
				Title:           outline.name,
				Topic:           provider.Topic,
				Tags:            tags,
				OccurrenceCount: 1,
			})
			if ok {
				collector.add(candidate)
			}
		case xml.EndElement:
			if value.Name.Local == "outline" && bodyDepth > 0 && len(outlines) > 0 {
				context := outlines[len(outlines)-1]
				outlines = outlines[:len(outlines)-1]
				if context.isCategory {
					categories = categories[:len(categories)-1]
				}
			}
			if value.Name.Local == "body" && bodyDepth > 0 {
				bodyDepth--
			}
			depth--
			if depth == 0 {
				rootClosed = true
			}
		}
	}
}

type opmlOutlineContext struct {
	isCategory bool
}

type opmlOutlineAttributes struct {
	name       string
	xmlURL     string
	htmlURL    string
	categories []string
}

func readOPMLOutline(start xml.StartElement) opmlOutlineAttributes {
	var text, title, xmlURL, htmlURL, category string
	for _, attribute := range start.Attr {
		switch attribute.Name.Local {
		case "text":
			text = attribute.Value
		case "title":
			title = attribute.Value
		case "xmlUrl":
			xmlURL = attribute.Value
		case "htmlUrl":
			htmlURL = attribute.Value
		case "category":
			category = attribute.Value
		}
	}
	name := strings.TrimSpace(title)
	if name == "" {
		name = strings.TrimSpace(text)
	}
	return opmlOutlineAttributes{
		name:       name,
		xmlURL:     strings.TrimSpace(xmlURL),
		htmlURL:    strings.TrimSpace(htmlURL),
		categories: splitOPMLCategories(category),
	}
}

func splitOPMLCategories(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
}
