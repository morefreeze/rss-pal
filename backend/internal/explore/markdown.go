package explore

import (
	"regexp"
	"strings"
)

// GitHubAwesomeAdapter reads Markdown link lists, retaining only public links
// that do not point back to GitHub or to image/badge hosts.
type GitHubAwesomeAdapter struct{}

func (GitHubAwesomeAdapter) Kind() string { return "github_awesome" }

func (GitHubAwesomeAdapter) Parse(provider Provider, body []byte) ([]Candidate, error) {
	if err := checkProviderBody(body); err != nil {
		return nil, err
	}
	text := string(body)
	collector := newCandidateCollector(maxProviderCandidates, func(candidate Candidate) string { return candidate.ExternalKey })
	for cursor := 0; cursor < len(text); {
		match := markdownLinkPattern.FindStringSubmatchIndex(text[cursor:])
		if match == nil {
			break
		}
		for index := range match {
			if match[index] >= 0 {
				match[index] += cursor
			}
		}
		cursor = match[1]
		if match[0] > 0 && text[match[0]-1] == '!' {
			continue
		}
		title := strings.TrimSpace(text[match[2]:match[3]])
		raw := strings.TrimSpace(text[match[4]:match[5]])
		if strings.Contains(raw, " ") {
			raw = strings.Fields(raw)[0]
		}
		normalized, ok := normalizePublicURL(raw)
		if !ok || ignoredAwesomeURL(normalized) {
			continue
		}
		domain := hostOf(normalized)
		collector.add(Candidate{ExternalKey: domain, FeedURL: normalized, Title: title, Topic: provider.Topic, Tags: []string{provider.Topic}, OccurrenceCount: 1})
	}
	return collector.candidates(), nil
}

var markdownLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

func ignoredAwesomeURL(raw string) bool {
	host := hostOf(raw)
	return host == "github.com" || strings.HasSuffix(host, ".github.com") ||
		host == "img.shields.io" || strings.HasSuffix(host, ".shields.io") ||
		isPrivateHost(host)
}
