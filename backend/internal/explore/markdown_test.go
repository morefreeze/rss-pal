package explore

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestGitHubAwesomeAdapterKeepsPublicExternalLinksOnly(t *testing.T) {
	const markdown = `
[Project](https://project.example/docs?utm_source=awesome)
[Duplicate](https://PROJECT.example/docs#top)
[Same Site](https://project.example/another-path)
[Repo](https://github.com/org/repo)
[Relative](./local.md)
![Badge](https://img.shields.io/badge/x-y-blue)
[Local](http://localhost:8080/feed)
[Private](http://127.0.0.1/feed)
[Mail](mailto:me@example.com)
`
	got, err := (GitHubAwesomeAdapter{}).Parse(Provider{Topic: "self-hosted"}, []byte(markdown))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(got), got)
	}
	if got[0].FeedURL != "https://project.example/another-path" || got[0].Title != "Same Site" || got[0].OccurrenceCount != 3 || got[0].ExternalKey != "project.example" {
		t.Errorf("candidate = %#v", got[0])
	}
}

func TestGitHubAwesomeAdapterBoundsDomainsAndIsOrderIndependent(t *testing.T) {
	links := make([]string, 0, 2101)
	for i := 2100; i >= 0; i-- {
		links = append(links, fmt.Sprintf("[site-%04d](https://site-%04d.example/path)", i, i))
	}
	forward := strings.Join(links, "\n")
	for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
		links[i], links[j] = links[j], links[i]
	}
	reversed := strings.Join(links, "\n")
	adapter := GitHubAwesomeAdapter{}
	got, err := adapter.Parse(Provider{Topic: "topic"}, []byte(forward))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	gotReversed, err := adapter.Parse(Provider{Topic: "topic"}, []byte(reversed))
	if err != nil {
		t.Fatalf("reversed Parse() error = %v", err)
	}
	if len(got) != 2000 || got[0].FeedURL != "https://site-0000.example/path" || got[len(got)-1].FeedURL != "https://site-1999.example/path" {
		t.Fatalf("candidates = %d, first/last = %#v / %#v", len(got), got[0], got[len(got)-1])
	}
	if !reflect.DeepEqual(got, gotReversed) {
		t.Fatal("Markdown candidates depend on link order")
	}
}

func TestGitHubAwesomeAdapterRejectsFourMiBPlusOne(t *testing.T) {
	_, err := (GitHubAwesomeAdapter{}).Parse(Provider{}, make([]byte, defaultProviderBodyBytes+1))
	if err == nil {
		t.Fatal("Parse() accepted body over four MiB")
	}
}
