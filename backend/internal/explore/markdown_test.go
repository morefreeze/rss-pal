package explore

import "testing"

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
