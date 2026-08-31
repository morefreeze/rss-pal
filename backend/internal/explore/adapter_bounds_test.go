package explore

import (
	"strings"
	"testing"
)

func TestExploreAdaptersShareFourMiBBodyGate(t *testing.T) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{"OPML", func() error {
			_, err := (OPMLRegistryAdapter{}).Parse(Provider{}, []byte(xmlBodyOverProviderLimit(`<opml><body>`, `</body></opml>`)))
			return err
		}},
		{"Directory", func() error {
			_, err := (DirectoryAdapter{}).Parse(Provider{}, []byte(xmlBodyOverProviderLimit(`<rss><channel>`, `</channel></rss>`)))
			return err
		}},
		{"Reddit", func() error {
			_, err := (RedditLinkStreamAdapter{}).Parse(Provider{}, []byte(xmlBodyOverProviderLimit(`<rss><channel>`, `</channel></rss>`)))
			return err
		}},
		{"Markdown", func() error {
			_, err := (GitHubAwesomeAdapter{}).Parse(Provider{}, []byte(strings.Repeat(" ", defaultProviderBodyBytes+1)))
			return err
		}},
		{"Related", func() error {
			_, err := (RelatedSiteDiscoverer{}).Discover("https://site.example/article", []byte(strings.Repeat(" ", defaultProviderBodyBytes+1)))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.parse(); err == nil {
				t.Fatal("accepted body over four MiB")
			}
		})
	}
}
